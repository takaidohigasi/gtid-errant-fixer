# gtid-errant-fixer

simple cli tool to fix errant gtid for MySQL Multi Source Replication(MSR)

You don't use MSR, and use orchestrator (>=3.0.14), it's OK to fix on it.
Please see also the detail on the Percona Live [slide](https://www.percona.com/live/19/sites/default/files/slides/Errant%20GTIDs%20Breaking%20Replication_%20How%20to%20Detect%20and%20Avoid%20Them%20-%20FileId%20-%20187306.pdf)

```
Usage of ./bin/gtid-errant-fixer:
  -c string
          mysql client config (default ".my.cnf")
            -f    force execution

```
