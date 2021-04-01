# gtid-errant-fixer

simple cli tool to fix errant gtid for MySQL Multi Source Replication(MSR)

If you don't use MSR, and use orchestrator (>=3.0.14), it's OK to fix on it.
Please see also the detail on the Percona Live [slide](https://www.percona.com/live/19/sites/default/files/slides/Errant%20GTIDs%20Breaking%20Replication_%20How%20to%20Detect%20and%20Avoid%20Them%20-%20FileId%20-%20187306.pdf)

## usage

```
$ ./gtid-errant-fixer --help
Usage of ./gtid-errant-fixer:
  -c string
        mysql client config(need SUPER priv to operate stop / slave) (default ".my.cnf")
  -f    force execution (skip prompt to confirm Y/N
  -p string
        mysql client config (need REPLICATION SLAVE)
  -u string
        mysql client config (need REPLICATION SLAVE)
```

## example

you can reset errant_gtid which you can confirm with server_id and host (report_host is required to enable)

```
$ ./gtid-errant-fixer
errant transaction pre-check:
 errant_gtid ea2c5164-930b-11eb-901c-0242c0a81004:1: server_id: 3, host mysql3
stopping replica
original gtid_executed:
e9f7ad21-930b-11eb-8646-0242c0a81002:1-18,
ea052114-930b-11eb-8dd3-0242c0a81003:1-18,
ea2c5164-930b-11eb-901c-0242c0a81004:1
would you continue to reset? (y/n) [n]: y
RESET SLAVE
RESET MASTER
SET GLOBAL gtid_purged='e9f7ad21-930b-11eb-8646-0242c0a81002:1-18,ea052114-930b-11eb-8dd3-0242c0a81003:1-18'
resuming replica
completed.
```
