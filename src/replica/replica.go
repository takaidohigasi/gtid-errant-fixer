package replica

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"strings"

	"github.com/Songmu/prompter"
	"github.com/jmoiron/sqlx"
)

type (
	ReplicaStatus struct {
		AutoPosition      bool   `db:"Auto_Position"`
		ChannelName       string `db:"Channel_Name"`
		ExecutedGtidSet   string `db:"Executed_Gtid_Set"`
		GtidExecuted      string // @@gtid_executed
		MasterHost        string `db:"Master_Host"`
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
		serverHosts map[string]*MySQLServerInfo
	}

	MySQLServerInfo struct {
		ServerId   int    `db:"Server_id"`
		Host       string `db:"Host"`
		serverUuid string `db:"SlaveUUID"`
	}
)

const (
	resetMasterQuery     = `RESET MASTER`
	resetReplicaQuery    = `RESET SLAVE`
	getserverUuidQuery   = `SELECT @@server_uuid`
	getGTIDExecutedQuery = `SELECT @@global.gtid_executed`
	// see https://dev.mysql.com/doc/refman/5.7/en/gtid-functions.html
	getErrantGTIDsQuery  = `SELECT GTID_SUBTRACT(%s, %s)`
	setGtidPurgedQuery   = `SET GLOBAL gtid_purged='%s'`
	replicaStatusQuery   = `SHOW SLAVE STATUS`
	showReplicaHostQuery = `SHOW SLAVE HOSTS`
	startReplicaQuery    = `START SLAVE`
	stopReplicaQuery     = `STOP SLAVE`
)

func NewMySQLDB(db *sql.DB, monitorUser string, monitorPass string) (*MySQLDB, error) {
	var serverUuid string
	if err := db.QueryRow(getserverUuidQuery).Scan(&serverUuid); err != nil {
		return nil, err
	}
	return &MySQLDB{db, nil, serverUuid, monitorUser, monitorPass, nil}, nil
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

func (db *MySQLDB) errantTransaction() ([]string, error) {
	fmt.Println("errant transaction pre-check: ")

	var errantGtidSets []string
	for _, replica := range db.replStatus {
		var errantGtidSet string

		// connect to maser
		dsn := db.monitorUser + ":" + db.monitorPass + "@(" + replica.MasterHost + ")/"
		fmt.Println(dsn)
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
		server := &MySQLServerInfo{}
		for rows.Next() {
			if err = rows.StructScan(server); err != nil {
				return errantGtidSets, err
			}
			db.serverHosts[server.serverUuid] = server
		}

		// gtid_executed
		if err := sqlxdb.QueryRowx(getGTIDExecutedQuery).Scan(&replica.GtidExecuted); err != nil {
			return errantGtidSets, err
		}
		if err := sqlxdb.QueryRowx(fmt.Sprintf(getErrantGTIDsQuery, replica.GtidExecuted, replica.ExecutedGtidSet)).Scan(&errantGtidSet); err != nil {
			return errantGtidSets, err
		}
		errantGtidSets = append(errantGtidSets, errantGtidSet)
	}

	for _, errant := range errantGtidSets {
		host := db.serverHosts[strings.Split(errant, ":")[0]]
		fmt.Printf("errant_gtid: gtid %s, host %s\n", errant, host.Host)
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

	if err := db.dbh.QueryRow(getserverUuidQuery).Scan(&db.serverUuid); err != nil {
		return err
	}

	errantGtidSets, err := db.errantTransaction()
	if err != nil {
		return err
	}

	if err := db.stopReplica(); err != nil {
		return err
	}
	defer db.resumeReplica()

	if err := db.gatherReplicaStatuses(); err != nil {
		return err
	}

	executedGtidSet := db.replStatus[0].ExecutedGtidSet
	fmt.Printf("original gtid_executed: \n%s\n", executedGtidSet)
	gtidSet := strings.Split(executedGtidSet, ",")

	var gtidPurged []string
	for _, gtid := range gtidSet {
		gtid = strings.Trim(gtid, "\n")
		for _, errant := range errantGtidSets {
			if strings.HasPrefix(gtid, strings.Split(errant, ":")[0]) {
				break
			}
		}
		gtidPurged = append(gtidPurged, gtid)
	}

	if !forceOption {
		if !prompter.YN("would you continue to reset?", false) {
			if !prompter.YN("would you continue to reset?", false) {
				fmt.Println("do nothing")
				return nil
			}
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
