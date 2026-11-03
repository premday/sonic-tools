package sonic

// Redis database indexes.
//
// source: https://github.com/sonic-net/SONiC/blob/master/doc/database/multi_database_instances.md
const (
	APPLDB        = 0
	ASICDB        = 1
	COUNTERSDB    = 2
	LOGLEVELDB    = 3
	CONFIGDB      = 4
	PFCWDDB       = 5
	FLEXCOUNTERDB = 5
	STATEDB       = 6
	SNMPOVERLAYDB = 7
)

const (
	RedisAddr = "127.0.0.1:6379"

	SONICDir     = "/etc/sonic/"
	ConfigDBFile = "/etc/sonic/config_db.json"
	FRRFile      = "/etc/sonic/frr/frr.conf"
	VersionFile  = "/etc/sonic/sonic_version.yml"
)
