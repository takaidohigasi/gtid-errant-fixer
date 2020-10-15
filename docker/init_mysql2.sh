#!/bin/sh

mysql -u root << EOT
CREATE DATABASE IF NOT EXISTS test2;
CREATE TABLE IF NOT EXISTS test2.test (
  id    int  NOT NULL AUTO_INCREMENT,
  time  DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(id)
);
EOT

# insert some data
for i in {1..10} ; do mysql -u root -e "INSERT INTO test2.test values ()" && sleep 1 ; done
