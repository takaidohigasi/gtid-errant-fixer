#!/bin/sh

# create sample table
mysql -u root << EOT
CREATE DATABASE IF NOT EXISTS test1;
CREATE TABLE IF NOT EXISTS test1.test (
  id    int  NOT NULL AUTO_INCREMENT,
  time  DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(id)
);
EOT

# insert some data
for i in {1..10} ; do mysql -u root -e "INSERT INTO test1.test values ()" && sleep 1 ; done
