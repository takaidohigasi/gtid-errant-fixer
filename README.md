# gtid-errant-fixer

simple cli tool to fix errant gtid for MySQL Multi Source Replication(MSR)

If you don't use MSR, and use orchestrator (>=3.0.14), it's OK to fix on it.
Please see also the detail on the Percona Live [slide](https://www.percona.com/live/19/sites/default/files/slides/Errant%20GTIDs%20Breaking%20Replication_%20How%20to%20Detect%20and%20Avoid%20Them%20-%20FileId%20-%20187306.pdf)

## usage

```
Usage of ./bin/gtid-errant-fixer:
  -c string
          mysql client config (default ".my.cnf")
            -f    force execution
```

## example

```
% ./bin/gtid-errant-fixer                                                                                                                 main
errant transaction pre-check: 
  errant transaction found: 211f4d80-914c-11eb-b1ec-0242ac1f0004:1
  stopping replica
  original gtid_executed: 
  0a59fb94-914c-11eb-8644-0242ac1f0002:1-4,
  0a66ee4a-914c-11eb-b206-0242ac1f0003:1-7,
  211f4d80-914c-11eb-b1ec-0242ac1f0004:1
  remove 211f4d80-914c-11eb-b1ec-0242ac1f0004:1 from gtid_executed
  would you continue to reset? (y/n) [n]: y
  RESET SLAVE
  RESET MASTER
  SET GLOBAL gtid_purged='0a59fb94-914c-11eb-8644-0242ac1f0002:1-4,0a66ee4a-914c-11eb-b206-0242ac1f0003:1-7'
  resuming replica
  completed.
```
