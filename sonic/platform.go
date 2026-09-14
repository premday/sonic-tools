package sonic

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	redis "github.com/redis/go-redis/v9"
)

// Platform identifies the device: metadata from CONFIG_DB, image version and serial number.
type Platform struct {
	Hostname string  `redis:"hostname" json:"hostname"`
	HwSKU    string  `redis:"hwsku" json:"hwsku"`
	Name     string  `redis:"platform" json:"platform"`
	MAC      string  `redis:"mac" json:"mac"`
	ASN      int     `redis:"bgp_asn" json:"asn"`
	Role     string  `redis:"type" json:"role"`
	Serial   string  `json:"serial"`
	Version  Version `json:"version"`
}

type Version struct {
	Build  string `yaml:"build_version" json:"build"`
	Debian string `yaml:"debian_version" json:"debian"`
	Kernel string `yaml:"kernel_version" json:"kernel"`
	ASIC   string `yaml:"asic_type" json:"asic"`
}

func PlatformInfo(ctx context.Context, rdb *redis.Client) (Platform, error) {
	conn, err := openDB(ctx, rdb, CONFIGDB)
	if err != nil {
		return Platform{}, err
	}
	defer conn.Close()

	platform := Platform{}
	if err := conn.HGetAll(ctx, "DEVICE_METADATA|localhost").Scan(&platform); err != nil {
		return Platform{}, fmt.Errorf("failed to get device metadata: %w", err)
	}

	platform.Version, err = ImageVersion()
	if err != nil {
		return platform, err
	}

	// the EEPROM is not readable on all platforms
	if serial, err := run(ctx, "decode-syseeprom", "-s"); err == nil {
		platform.Serial = strings.TrimSpace(serial)
	}

	return platform, nil
}

func ImageVersion() (Version, error) {
	data, err := os.ReadFile(VersionFile)
	if err != nil {
		return Version{}, fmt.Errorf("failed to read %s: %w", VersionFile, err)
	}

	version := Version{}
	if err := yaml.Unmarshal(data, &version); err != nil {
		return Version{}, fmt.Errorf("failed to parse %s: %w", VersionFile, err)
	}

	return version, nil
}

type PSU struct {
	Index     string  `json:"index"`
	Name      string  `json:"name"`
	Presence  NABool  `json:"presence"`
	Status    NABool  `json:"status"`
	LedStatus string  `json:"led_status"`
	Model     string  `json:"model"`
	Serial    string  `json:"serial"`
	Revision  string  `json:"revision"`
	Voltage   NAFloat `json:"voltage"`
	Current   NAFloat `json:"current"`
	Power     NAFloat `json:"power"`
}

func PSUStatus(ctx context.Context) ([]PSU, error) {
	psu := []PSU{}
	if err := runJSON(ctx, &psu, "psushow", "-s", "--json"); err != nil {
		return nil, err
	}
	return psu, nil
}

type Fan struct {
	Name       string  `json:"name"`
	DrawerName string  `redis:"drawer_name" json:"drawer_name"`
	Status     NABool  `redis:"status" json:"status"`
	Presence   NABool  `redis:"presence" json:"presence"`
	LedStatus  string  `redis:"led_status" json:"led_status"`
	Speed      NAFloat `redis:"speed" json:"speed"`
	Direction  string  `redis:"direction" json:"direction"`
	Timestamp  string  `redis:"timestamp" json:"timestamp"`
}

func FanStatus(ctx context.Context, rdb *redis.Client) ([]Fan, error) {
	conn, err := openDB(ctx, rdb, STATEDB)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	keys, err := scanKeys(ctx, conn, "FAN_INFO|*")
	if err != nil {
		return nil, err
	}
	sortNames(keys)

	fans := make([]Fan, 0, len(keys))
	for _, key := range keys {
		fan := Fan{Name: strings.TrimPrefix(key, "FAN_INFO|")}
		if err := conn.HGetAll(ctx, key).Scan(&fan); err != nil {
			return nil, fmt.Errorf("failed to get %s: %w", key, err)
		}
		fans = append(fans, fan)
	}

	return fans, nil
}

type Temperature struct {
	Name              string  `json:"name"`
	Temperature       NAFloat `redis:"temperature" json:"temperature"`
	HighThreshold     NAFloat `redis:"high_threshold" json:"high_threshold"`
	CriticalThreshold NAFloat `redis:"critical_high_threshold" json:"critical_threshold"`
	// Warning is true when the sensor crossed its high threshold, it is null when the device does not report it.
	Warning   *bool  `redis:"warning_status" json:"warning"`
	Timestamp string `redis:"timestamp" json:"timestamp"`
}

func TemperatureStatus(ctx context.Context, rdb *redis.Client) ([]Temperature, error) {
	conn, err := openDB(ctx, rdb, STATEDB)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	keys, err := scanKeys(ctx, conn, "TEMPERATURE_INFO|*")
	if err != nil {
		return nil, err
	}
	sortNames(keys)

	temperatures := make([]Temperature, 0, len(keys))
	for _, key := range keys {
		temperature := Temperature{Name: strings.TrimPrefix(key, "TEMPERATURE_INFO|")}
		if err := conn.HGetAll(ctx, key).Scan(&temperature); err != nil {
			return nil, fmt.Errorf("failed to get %s: %w", key, err)
		}
		temperatures = append(temperatures, temperature)
	}

	return temperatures, nil
}

// NAFloat is a measure which is null when the device does not report it, as SONiC writes
// 'N/A' for a sensor it cannot read.
type NAFloat struct {
	Value *float64
}

// Measure returns a reported measure, it is only needed to build a value outside of a device.
func Measure(value float64) NAFloat {
	return NAFloat{Value: &value}
}

func (nf *NAFloat) UnmarshalText(text []byte) error {
	value, err := strconv.ParseFloat(strings.TrimSpace(string(text)), 64)
	// SONiC writes 'inf' or '-inf' for an optical power a transceiver cannot measure
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		nf.Value = nil
		return nil //nolint:nilerr // an unreadable measure is null, not a failure
	}
	nf.Value = &value

	return nil
}

func (nf NAFloat) MarshalJSON() ([]byte, error) {
	if nf.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*nf.Value)
}

// String returns the measure with two decimals, and nothing when it is not reported.
func (nf NAFloat) String() string {
	if nf.Value == nil {
		return ""
	}
	return strconv.FormatFloat(*nf.Value, 'f', 2, 64)
}

// NABool is a boolean which is null when the device reports an unknown value.
type NABool struct {
	*bool
}

func (nb *NABool) UnmarshalText(text []byte) error {
	value := true
	switch strings.ToLower(string(text)) {
	case "true", "ok":
		nb.bool = &value
	case "false", "not ok":
		value = false
		nb.bool = &value
	default:
		nb.bool = nil
	}
	return nil
}

func (nb NABool) MarshalJSON() ([]byte, error) {
	if nb.bool == nil {
		return []byte("null"), nil
	}
	if *nb.bool {
		return []byte("true"), nil
	}
	return []byte("false"), nil
}

func (nb NABool) String() string {
	if nb.bool == nil {
		return "N/A"
	}
	if *nb.bool {
		return "OK"
	}
	return "not OK"
}
