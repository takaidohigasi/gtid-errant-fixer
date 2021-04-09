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
	ReplicaNode struct {
		AutoPosition      bool   `db:"Auto_Position"`
		ChannelName       string `db:"Channel_Name"`
		ExecutedGtidSet   string `db:"Executed_Gtid_Set"`
		GtidExecuted      string // @@gtid_executed
		Info              MySQLServerInfo
		Level             int
		Source            *ReplicaNode
		MasterHost        string `db:"Master_Host"`
		MasterPort        int    `db:"Master_Port"`
		MasterUUID        string `db:"Master_UUID"`
		ReplicaIORunning  string `db:"Slave_IO_Running"`
		ReplicaSQLRunning string `db:"Slave_SQL_Running"`
	}

	MySQLDB struct {
		dbh             *sql.DB
		executedGtidSet string
		monitorUser     string
		monitorPass     string
		replicaNodes    map[string]ReplicaNode
		serverUuid      string
	}

	MySQLServerInfo struct {
		ServerId   int    `db:"Server_id"`
		Host       string `db:"Host"`
		ServerUuid string `db:"Slave_UUID"`
	}
)

const (
	resetMasterQuery  = `RESET MASTER`
	resetReplicaQuery = `RESET SLAVE`
	// see https://dev.mysql.com/doc/refman/5.7/en/gtid-functions.html
	getErrantGTIDsQuery    = `SELECT GTID_SUBTRACT('%s', '%s')`
	getGTIDExecutedQuery   = `SELECT @@global.gtid_executed`
	getServerUuidQuery     = `SELECT @@server_uuid`
	setGtidPurgedQuery     = `SET GLOBAL gtid_purged='%s'`
	showReplicaHostQuery   = `SHOW SLAVE HOSTS`
	showReplicaStatusQuery = `SHOW SLAVE STATUS`
	startReplicaQuery      = `START SLAVE`
	stopReplicaQuery       = `STOP SLAVE`
)

func NewMySQLDB(db *sql.DB, monitorUser string, monitorPass string) (*MySQLDB, error) {
	var serverUuid string
	if err := db.QueryRow(getServerUuidQuery).Scan(&serverUuid); err != nil {
		return nil, err
	}
	return &MySQLDB{
		db,
		"",
		monitorUser,
		monitorPass,
		map[string]ReplicaNode{},
		serverUuid,
	}, nil
}

func (node ReplicaNode) searchNode(db *MySQLDB) error {
	var sqlxDb *sqlx.DB

	if node.MasterHost == "" {
		sqlxDb = sqlx.NewDb(db.dbh, "mysql")
	} else {
		sqlxDb, err := node.DB(db.monitorUser, db.monitorPass)
		if err != nil {
			return err
		}
		defer sqlxDb.Close()
	}

	rows, err := sqlxDb.Unsafe().Queryx(showReplicaStatusQuery)
	if err != nil {
		return err
	}

	// if the node is treetop
	if !rows.NextResultSet() {
		n := db.replicaNodes[node.MasterUUID]
		n.updateSelfInfo()
		return nil
	}

	// if the node is not treetop
	newNode := ReplicaNode{}
	rows, err = sqlxDb.Unsafe().Queryx(showReplicaStatusQuery)
	if err != nil {
		return err
	}
	for rows.Next() {
		if err = rows.StructScan(&newNode); err != nil {
			return err
		}
		newNode.Level = node.Level + 1
		newNode.Source = &node
		db.replicaNodes[newNode.MasterUUID] = newNode
		if err := newNode.updateDownstreamInfo(db); err != nil {
			return err
		}

		// recursive search
		if err := newNode.searchNode(db); err != nil {
			return err
		}
	}

	return nil
}
func (node ReplicaNode) updateSelfInfo() error {
	return nil
}

func (node ReplicaNode) DB(monitorUser string, monitorPass string) (*sqlx.DB, error) {
	dsn := monitorUser + ":" + monitorPass + "@(" + node.MasterHost + ":" + strconv.Itoa(node.MasterPort) + ")/"
	return sqlx.Open("mysql", dsn)
}

func (node ReplicaNode) updateDownstreamInfo(db *MySQLDB) error {
	// collect replication node info
	sqlxDb, err := node.DB(db.monitorUser, db.monitorPass)
	if err != nil {
		return err
	}
	defer sqlxDb.Close()

	rows, err := sqlxDb.Unsafe().Queryx(showReplicaHostQuery)
	if err != nil {
		return err
	}
	server := MySQLServerInfo{}
	for rows.Next() {
		if err = rows.StructScan(&server); err != nil {
			return err
		}
		if val, ok := db.replicaNodes[server.ServerUuid]; ok {
			val.Info = server
		} else {
			node := ReplicaNode{}
			node.Info = server
			db.replicaNodes[server.ServerUuid] = node
		}
	}

	return nil
}

// gatherReplicaNodeses update replStatus of MySQLDB
func (db *MySQLDB) gatherReplicaStatus() error {
	node := ReplicaNode{}
	if _, ok := db.replicaNodes[db.serverUuid]; !ok {
		node.Level = 0
		node.MasterUUID = db.serverUuid
		if err := node.updateSelfInfo(); err != nil {
			return err
		}
		db.replicaNodes[db.serverUuid] = node
	}

	if err := node.searchNode(db); err != nil {
		return err
	}

	for _, node := range db.replicaNodes {
		if node.Level == 1 {
			db.executedGtidSet = node.ExecutedGtidSet
			break
		}
	}
	return nil
}

// function autoPosition check whether auto position is enabled for all the channel
func (db *MySQLDB) autoPosition() bool {
	for _, node := range db.replicaNodes {
		if node.Level == 2 && !node.AutoPosition {
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
	for _, node := range db.replicaNodes {
		if node.Level == 1 {
			sqlxDb, err := node.DB(db.monitorUser, db.monitorPass)
			if err != nil {
				return "", err
			}
			defer sqlxDb.Close()

			// gtid_executed
			if err := sqlxDb.QueryRowx(getGTIDExecutedQuery).Scan(&node.GtidExecuted); err != nil {
				return "", err
			}
			executedGtidSets = append(executedGtidSets, node.GtidExecuted)
		}
	}
	replicaGtidSets := strings.Replace(db.executedGtidSet, "\n", "", -1)
	masterGtidSets := strings.Join(executedGtidSets, ",")
	sqlxDb := sqlx.NewDb(db.dbh, "mysql")
	if err := sqlxDb.QueryRowx(fmt.Sprintf(getErrantGTIDsQuery, replicaGtidSets, masterGtidSets)).Scan(&errantGtidSets); err != nil {
		return errantGtidSets, err
	}

	if errantGtidSets != "" {
		for _, errant := range strings.Split(errantGtidSets, ",") {
			errant = strings.Replace(errant, "\n", "", -1)
			node := db.replicaNodes[strings.Split(errant, ":")[0]]
			fmt.Printf(" errant_gtid %s: server_id: %d, host %s\n", errant, node.Info.ServerId, node.Info.Host)
		}
	}
	fmt.Println("")

	return errantGtidSets, nil
}

func (db *MySQLDB) FixErrantGTID(forceOption bool) error {
	if err := db.gatherReplicaStatus(); err != nil {
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

	if err := db.gatherReplicaStatus(); err != nil {
		return err
	}

	// print original state just in case
	executedGtidSet := db.executedGtidSet
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

	if !forceOption && !prompter.YN("\nWould you continue to reset?", false) {
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

	return nil
}
