package main

import (
	"flag"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/sjmudd/mysql_defaults_file"
	"os"

	"github.com/takaidohigasi/gtid-errant-fixer/src/replica"
)

func exitWithError(err error) {
	fmt.Print(err)
	os.Exit(1)
}

func main() {
	var conf string
	var forceOption bool

	flag.StringVar(&conf, "c", ".my.cnf", "mysql client config")
	flag.BoolVar(&forceOption, "f", false, "force execution")
	flag.Parse()

	_, err := os.Stat(conf)
	if err != nil {
		exitWithError(err)
	}

	db, err := mysql_defaults_file.OpenUsingDefaultsFile("mysql", conf, "")
	if err != nil {
		exitWithError(err)
	}
	defer db.Close()

	mysqlDB := replica.MySQLDB{db, nil, ""}
	if err := mysqlDB.FixErrantGTID(forceOption); err != nil {
		exitWithError(err)
	}
	fmt.Println("completed.")
}
