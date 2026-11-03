package sonic

import (
	"context"
	"fmt"
	"strings"

	redis "github.com/redis/go-redis/v9"
)

// SNMPConfig returns the SNMP tables of CONFIG_DB, keyed by table then entry name.
// A table which is not configured is left out.
func SNMPConfig(ctx context.Context, rdb *redis.Client) (map[string]any, error) {
	conn, err := openDB(ctx, rdb, CONFIGDB)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	config := map[string]any{}
	for _, table := range []string{"SNMP", "SNMP_COMMUNITY"} {
		keys, err := scanKeys(ctx, conn, fmt.Sprintf("%s|*", table))
		if err != nil {
			return nil, err
		}
		if len(keys) == 0 {
			continue
		}

		entries := map[string]map[string]string{}
		for _, key := range keys {
			fields, err := conn.HGetAll(ctx, key).Result()
			if err != nil {
				return nil, fmt.Errorf("failed to read %s: %w", key, err)
			}
			_, name, _ := strings.Cut(key, "|")
			entries[name] = fields
		}
		config[table] = entries
	}

	return config, nil
}
