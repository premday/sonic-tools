package fetcher

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	redis "github.com/redis/go-redis/v9"
)

type SONipremconfigDBPORT struct {
	AdminStatus string `redis:"admin_status"`
	Alias       string `redis:"alias"`
	Description string `redis:"description"`
	FEC         string `redis:"fec"`
	Index       int    `redis:"index"`
	Lanes       string `redis:"lanes"`
	MTU         int    `redis:"mtu"`
	Speed       int    `redis:"speed"`
}

// GetInterfaceInformation get CONFIG_DB PORT|"interface" info from redis.
func GetInterfaceInformation(ctx context.Context, rdb *redis.Client, intf string) (SONipremconfigDBPORT, error) {
	conn := rdb.Conn()
	defer conn.Close()
	if err := conn.Select(ctx, CONFIGDB).Err(); err != nil {
		return SONipremconfigDBPORT{}, fmt.Errorf("failed to select CONFIG_DB: %w", err)
	}
	var info SONipremconfigDBPORT
	err := conn.HGetAll(ctx, fmt.Sprintf("PORT|%s", intf)).Scan(&info)

	return info, err
}

func GetIPInterface(ctx context.Context, rdb *redis.Client, intf string) ([]netip.Prefix, error) {
	conn := rdb.Conn()
	defer conn.Close()

	if err := conn.Select(ctx, APPLDB).Err(); err != nil {
		return nil, fmt.Errorf("failed to select APPL_DB: %w", err)
	}
	keyPrefix := fmt.Sprintf("INTF_TABLE:%s:", intf)
	key := fmt.Sprintf("%s*", keyPrefix)
	iter := conn.Scan(ctx, 0, key, 0).Iterator()
	localAddresses := []netip.Prefix{}

	for iter.Next(ctx) {
		k := iter.Val()
		prefix, err := netip.ParsePrefix(strings.TrimPrefix(k, keyPrefix))
		if err == nil {
			localAddresses = append(localAddresses, prefix)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failure while getting IP of interfaces from redis: %w", err)
	}

	return localAddresses, nil
}

type InterfaceAddrs struct {
	Name   string
	Prefix netip.Prefix
}

func FetchInterfacesAddrs(ctx context.Context, rdb *redis.Client) ([]InterfaceAddrs, error) {
	conn := rdb.Conn()
	defer conn.Close()

	if err := conn.Select(ctx, APPLDB).Err(); err != nil {
		return nil, fmt.Errorf("failed to select APPL_DB: %w", err)
	}
	iter := conn.Scan(ctx, 0, "INTF_TABLE:*:*", 0).Iterator()
	interfaces := []InterfaceAddrs{}

	r := regexp.MustCompile("INTF_TABLE:([^:]+):(.*)")
	for iter.Next(ctx) {
		k := iter.Val()
		entries := r.FindSubmatch([]byte(k))
		if len(entries) != 3 {
			return nil, fmt.Errorf("'%s' must have only one exact match", k)
		}
		prefix, err := netip.ParsePrefix(string(entries[2]))
		if err != nil {
			return nil, fmt.Errorf("'%s' missing network in redis", entries)
		}

		interfaces = append(interfaces, InterfaceAddrs{string(entries[1]), prefix})
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failure while getting IP of interfaces from redis: %w", err)
	}

	return interfaces, nil
}

func FetchInterfaceNeighbors(ctx context.Context, rdb *redis.Client) (map[string]string, error) {
	conn := rdb.Conn()
	defer conn.Close()
	if err := conn.Select(ctx, CONFIGDB).Err(); err != nil {
		return nil, fmt.Errorf("failed to select CONFIG_DB: %w", err)
	}

	iter := conn.Scan(ctx, 0, "DEVICE_NEIGHBOR|*", 0).Iterator()
	interfaceDescriptions := make(map[string]string)

	r := regexp.MustCompile(`DEVICE_NEIGHBOR\|(Ethernet[0-9]+)`)
	for iter.Next(ctx) {
		key := iter.Val()
		entries := r.FindSubmatch([]byte(key))
		if len(entries) != 2 {
			continue
		}

		interfaceName := string(entries[1])

		description := conn.HGet(ctx, key, "name").Val()
		interfaceDescriptions[interfaceName] = description
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failure while getting interface descriptions from redis: %w", err)
	}

	return interfaceDescriptions, nil
}
