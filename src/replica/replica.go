package replica

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	_ "github.com/go-sql-driver/mysql"

	"github.com/jmoiron/sqlx"
	"github.com/Songmu/prompter"
)

type (
	ReplicaStatus struct {
		AutoPosition      bool   `db:"Auto_Position"`
		ChannelName       string `db:"Channel_Name"`
		ExecutedGtidSet   string `db:"Executed_Gtid_Set"`
		MasterHost        string `db:"Master_Host"`
		MasterUUID        string `db:"Master_UUID"`
		ReplicaIORunning  string `db:"Slave_IO_Running"`
		ReplicaSQLRunning string `db:"Slave_SQL_Running"`
	}

	MySQLDB struct {
		Dbh        *sql.DB
		ReplStatus []ReplicaStatus
		ServerUuid string
	}
)

const (
	replicaStatusQuery   = `SHOW SLAVE STATUS`
	stopReplicaQuery     = `STOP SLAVE`
	startReplicaQuery    = `START SLAVE`
	resetReplicaQuery    = `RESET SLAVE`
	resetMasterQuery     = `RESET MASTER`
	setGtidPurgedQuery   = `SET GLOBAL gtid_purged='%s'`
	getServerUuidQuery   = `SELECT @@server_uuid`
)

// gatherReplicaStatuses update ReplStatus of MySQLDB
func (db *MySQLDB) gatherReplicaStatuses() error {
	sqlxDb := sqlx.NewDb(db.Dbh, "mysql")

	rows, err := sqlxDb.Unsafe().Queryx(replicaStatusQuery)
	if err != nil {
		return err
	}

	db.ReplStatus = nil
	status := &ReplicaStatus{}
	for rows.Next() {
		err = rows.StructScan(status)
		if err != nil {
			return err
		}
		db.ReplStatus = append(db.ReplStatus, *status)
	}

	return nil
}

// function autoPosition check whether auto position is enabled for all the channel
func (db *MySQLDB) autoPosition() bool {
	for _, status := range db.ReplStatus {
		if !status.AutoPosition {
			return false
		}
	}
	return true
}

func (db *MySQLDB) replicaStopped() bool {
	for _, status := range db.ReplStatus {
		if !(status.ReplicaIORunning == "No" && status.ReplicaSQLRunning != "No") {
			fmt.Printf("SlaveIORunning: %s, SlaveSQLRunning: %s (channel: %s)", status.ReplicaIORunning, status.ReplicaSQLRunning, status.ChannelName)
			return false
		}
	}
	return true
}

func (db *MySQLDB) stopReplica() error {
	fmt.Println("stopping replica")
    if _, err := db.Dbh.Exec(stopReplicaQuery); err !=nil {
		return err
	}

	return nil
}

func (db *MySQLDB) resumeReplica() error {
	fmt.Println("resuming replica")
	if _, err := db.Dbh.Exec(startReplicaQuery); err !=nil {
		return err
	}
	return nil
}

func (db *MySQLDB) errantTransaction() (bool) {
	fmt.Print("errant transaction pre-check: ")

	// warning: this is not strict check.
	for _, gtid := range strings.Split(db.ReplStatus[0].ExecutedGtidSet, ",") {
		if strings.HasPrefix(strings.Trim(gtid, "\n"), db.ServerUuid) {
			fmt.Printf("errant transaction found: %s\n", gtid)
			// TODO: print binlog events
			return true
		}
	}
	fmt.Println("no errant transaction")
	return false
}

func (db *MySQLDB) FixErrantGTID(forceOption bool) error {
	if err := db.gatherReplicaStatuses(); err != nil {
		return err
	}

	if !db.autoPosition() {
		return errors.New("auto position must be enabled for all the channel")
	}

	if err := db.Dbh.QueryRow(getServerUuidQuery).Scan(&db.ServerUuid); err != nil {
		return err
	}
	fmt.Printf("server_uuid: %s\n", db.ServerUuid)

	if !db.errantTransaction() {
		fmt.Println("no errant uuid.")
		return nil
	}

	if err := db.stopReplica(); err != nil {
		return err
	}
	defer db.resumeReplica()

	if err := db.gatherReplicaStatuses(); err != nil {
		return err
	}

	executedGtidSet := db.ReplStatus[0].ExecutedGtidSet
	fmt.Printf("original gtid_executed: \n%s\n", executedGtidSet)

	gtidSet := strings.Split(executedGtidSet, ",")

	var gtidPurged []string
	for _, gtid := range gtidSet {
		if !strings.HasPrefix(gtid, db.ServerUuid) {
			gtidPurged = append(gtidPurged, gtid)
		} else {
			fmt.Printf("remove %s from gtid_executed\n", gtid)
		}
	}

	if !forceOption {
		if prompter.YN("would you continue to reset?", false) {
			return nil
		}
	}
	if _, err := db.Dbh.Exec(resetReplicaQuery); err != nil {
			return err
		}
	if _, err := db.Dbh.Exec(resetMasterQuery); err != nil {
			return err
		}
	if _, err := db.Dbh.Exec(fmt.Sprintf(setGtidPurgedQuery, strings.Join(gtidPurged, ","))); err != nil {
			return err
		}

	return nil
}
