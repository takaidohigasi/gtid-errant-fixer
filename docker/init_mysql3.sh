#!/bin/sh

# mysql3 is replica of mysql1, and intermediate master of mysql4

sources=(
  # host table replication_channel
  "mysql1 test1 channel1"
)

while ! mysqladmin ping -h mysql1 --silent ; do
    sleep 1
done

gtids=()

mysql -u root -e "RESET SLAVE ALL"

 for server in "${sources[@]}"; do
     s=(${server})

     mysqldump -u root -h ${s[0]} ${s[1]} -B --single-transaction --flush-logs --events > /tmp/master_dump_${s[1]}.sql

     echo "restore for ${s[0]} / ${s[1]}"
     mysql -u root -e "RESET MASTER"
     mysql -u root < /tmp/master_dump_${s[1]}.sql

     # save gtid and reset / set all the ids at the end of the script
     gtid=$(mysql -u root -ss -e "SELECT @@global.gtid_executed")
     gtids=("${gtids[@]}" $gtid)

     mysql -u root -e "CHANGE MASTER TO MASTER_HOST='${s[0]}', MASTER_USER='root', MASTER_PASSWORD='', MASTER_AUTO_POSITION=1 FOR CHANNEL '${s[2]}'"
 done

gtidstr=$(IFS=,; echo "${gtids[*]}")
mysql -u root -e "RESET MASTER; SET GLOBAL gtid_purged='$gtidstr'; START SLAVE"
mysql -u root -e "CREATE USER 'readiness'@'%'"
