package replica

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Songmu/prompter"
	"github.com/jmoiron/sqlx"

	_ "github.com/go-sql-driver/mysql"
)

type (
	ReplicaStatus struct {
		AutoPosition      bool   `db:"Auto_Position"`
		ChannelName       string `db:"Channel_Name"`
		ExecutedGtidSet   string `db:"Executed_Gtid_Set"`
		GtidExecuted      string // @@gtid_executed
		MasterHost        string `db:"Master_Host"`
		MasterPort        int    `db:"Master_Port"`
		MasterUUID        string `db:"Master_UUID"`
		ReplicaIORunning  string `db:"Slave_IO_Running"`
		ReplicaSQLRunning string `db:"Slave_SQL_Running"`
	}

	MySQLDB struct {
		dbh         *sql.DB
		replStatus  []ReplicaStatus
		serverUuid  string
		monitorUser string
		monitorPass string
		serverHosts map[string]MySQLServerInfo
	}

	MySQLServerInfo struct {
		ServerId   int    `db:"Server_id"`
		Host       string `db:"Host"`
		ServerUuid string `db:"Slave_UUID"`
	}
)

const (
	resetMasterQuery     = `RESET MASTER`
	resetReplicaQuery    = `RESET SLAVE`
	getErrantGTIDsQuery  = `SELECT GTID_SUBTRACT('%s', '%s')`
	getGTIDExecutedQuery = `SELECT @@global.gtid_executed`
	getServerUuidQuery   = `SELECT @@server_uuid`
	// see https://dev.mysql.com/doc/refman/5.7/en/gtid-functions.html
	setGtidPurgedQuery   = `SET GLOBAL gtid_purged='%s'`
	replicaStatusQuery   = `SHOW SLAVE STATUS`
	showReplicaHostQuery = `SHOW SLAVE HOSTS`
	startReplicaQuery    = `START SLAVE`
	stopReplicaQuery     = `STOP SLAVE`
)

func NewMySQLDB(db *sql.DB, monitorUser string, monitorPass string) (*MySQLDB, error) {
	var serverUuid string
	if err := db.QueryRow(getServerUuidQuery).Scan(&serverUuid); err != nil {
		return nil, err
	}
	return &MySQLDB{db, nil, serverUuid, monitorUser, monitorPass, map[string]MySQLServerInfo{}}, nil
}

// gatherReplicaStatuses update replStatus of MySQLDB
func (db *MySQLDB) gatherReplicaStatuses() error {
	sqlxDb := sqlx.NewDb(db.dbh, "mysql")

	rows, err := sqlxDb.Unsafe().Queryx(replicaStatusQuery)
	if err != nil {
		return err
	}

	db.replStatus = nil
	status := &ReplicaStatus{}
	for rows.Next() {
		if err = rows.StructScan(status); err != nil {
			return err
		}
		db.replStatus = append(db.replStatus, *status)
	}

	return nil
}

// function autoPosition check whether auto position is enabled for all the channel
func (db *MySQLDB) autoPosition() bool {
	for _, status := range db.replStatus {
		if !status.AutoPosition {
			return false
		}
	}
	return true
}

func (db *MySQLDB) replicaStopped() bool {
	for _, status := range db.replStatus {
		if !(status.ReplicaIORunning == "No" && status.ReplicaSQLRunning != "No") {
			fmt.Printf("SlaveIORunning: %s, SlaveSQLRunning: %s (channel: %s)", status.ReplicaIORunning, status.ReplicaSQLRunning, status.ChannelName)
			return false
		}
	}
	return true
}

func (db *MySQLDB) stopReplica() error {
	fmt.Println("stopping replica")
	if _, err := db.dbh.Exec(stopReplicaQuery); err != nil {
		return err
	}

	return nil
}

func (db *MySQLDB) resumeReplica() error {
	fmt.Println("resuming replica")
	if _, err := db.dbh.Exec(startReplicaQuery); err != nil {
		return err
	}
	return nil
}

func (db *MySQLDB) errantTransaction() (string, error) {
	fmt.Println("errant transaction pre-check: ")

	var errantGtidSets string
	var executedGtidSets []string
	for _, replica := range db.replStatus {
		// connect to maser
		dsn := db.monitorUser + ":" + db.monitorPass + "@(" + replica.MasterHost + ":" + strconv.Itoa(replica.MasterPort) + ")/"
		sqlxdb, err := sqlx.Open("mysql", dsn)
		if err != nil {
			return errantGtidSets, err
		}
		defer sqlxdb.Close()

		// replication node info
		rows, err := sqlxdb.Unsafe().Queryx(showReplicaHostQuery)
		if err != nil {
			return errantGtidSets, err
		}
		server := MySQLServerInfo{}
		for rows.Next() {
			if err = rows.StructScan(&server); err != nil {
				return errantGtidSets, err
			}
			db.serverHosts[server.ServerUuid] = server
		}

		// gtid_executed
		if err := sqlxdb.QueryRowx(getGTIDExecutedQuery).Scan(&replica.GtidExecuted); err != nil {
			return errantGtidSets, err
		}
		executedGtidSets = append(executedGtidSets, replica.GtidExecuted)
	}

	replicaGtidSets := strings.Replace(db.replStatus[0].ExecutedGtidSet, "\n", "", -1)
	masterGtidSets := strings.Join(executedGtidSets, ",")
	sqlxdb := sqlx.NewDb(db.dbh, "mysql")
	if err := sqlxdb.QueryRowx(fmt.Sprintf(getErrantGTIDsQuery, replicaGtidSets, masterGtidSets)).Scan(&errantGtidSets); err != nil {
		return errantGtidSets, err
	}

	if errantGtidSets != "" {
		for _, errant := range strings.Split(errantGtidSets, ",") {
			host := db.serverHosts[strings.Split(errant, ":")[0]]
			fmt.Printf(" errant_gtid %s: server_id: %d, host %s\n", errant, host.ServerId, host.Host)
		}
	}

	return errantGtidSets, nil
}

func (db *MySQLDB) FixErrantGTID(forceOption bool) error {
	if err := db.gatherReplicaStatuses(); err != nil {
		return err
	}

	if !db.autoPosition() {
		return errors.New("auto position must be enabled for all the channel")
	}

	errantGtidSets, err := db.errantTransaction()
	if err != nil {
		return err
	}
	if errantGtidSets == "" {
		fmt.Println("errant GTID not found")
		return nil
	}

	if err := db.stopReplica(); err != nil {
		return err
	}
	defer db.resumeReplica()

	if err := db.gatherReplicaStatuses(); err != nil {
		return err
	}

	// print original state just in case
	executedGtidSet := db.replStatus[0].ExecutedGtidSet
	fmt.Printf("original gtid_executed: \n%s\n", executedGtidSet)
	gtidSet := strings.Split(executedGtidSet, ",")

	var gtidPurged []string
	for _, gtid := range gtidSet {
		gtid = strings.Replace(gtid, "\n", "", -1)
		errantFound := false
		for _, errant := range strings.Split(errantGtidSets, ",") {
			if strings.HasPrefix(gtid, strings.Split(errant, ":")[0]) {
				errantFound = true
			}
		}
		if !errantFound {
			gtidPurged = append(gtidPurged, gtid)
		}
	}

	if !forceOption {
		if !prompter.YN("would you continue to reset?", false) {
			fmt.Println("do nothing")
			return nil
		}
		fmt.Println(resetReplicaQuery)
		if _, err := db.dbh.Exec(resetReplicaQuery); err != nil {
			return err
		}
		fmt.Println(resetMasterQuery)
		if _, err := db.dbh.Exec(resetMasterQuery); err != nil {
			return err
		}
		fmt.Println(fmt.Sprintf(setGtidPurgedQuery, strings.Join(gtidPurged, ",")))
		if _, err := db.dbh.Exec(fmt.Sprintf(setGtidPurgedQuery, strings.Join(gtidPurged, ","))); err != nil {
			return err
		}
	}
	return nil
}
