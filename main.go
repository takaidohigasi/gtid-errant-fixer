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
	var conf, monitorUser, monitorPass string
	var forceOption bool

	flag.StringVar(&conf, "c", ".my.cnf", "mysql client config(need SUPER priv to operate stop / slave)")
	flag.StringVar(&monitorUser, "u", "root", "mysql client config ()")
	flag.StringVar(&monitorPass, "p", "", "mysql client config ()")
	flag.BoolVar(&forceOption, "f", false, "force execution (skip prompt to confirm Y/N")
	flag.Parse()

	if _, err := os.Stat(conf); err != nil {
		exitWithError(err)
	}
	db, err := mysql_defaults_file.OpenUsingDefaultsFile("mysql", conf, "")
	if err != nil {
		exitWithError(err)
	}
	defer db.Close()

	mysqlDB, err := replica.NewMySQLDB(db, monitorUser, monitorPass)
	if err != nil {
		exitWithError(err)
	}

	if err := mysqlDB.FixErrantGTID(forceOption); err != nil {
		exitWithError(err)
	}
	fmt.Println("completed.")
}
