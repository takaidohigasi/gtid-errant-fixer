#!/bin/sh

# mysql4 is replica of mysql2 and mysql3

sources=(
  # host table replication_channel
  "mysql3 test1 channel1"
  "mysql2 test2 channel2"
)


while ! mysqladmin ping -h mysql3 -u readiness --silent ; do
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
    gtid=$(mysql -u root -ss -e "SELECT @@global.gtid_executed" | sed -e 's/\\n//g')
    echo "gtid for ${s[0]} $gtid"
    gtids=("${gtids[@]}" $gtid)

    mysql -u root -e "CHANGE MASTER TO MASTER_HOST='${s[0]}', MASTER_USER='root', MASTER_PASSWORD='', MASTER_AUTO_POSITION=1 FOR CHANNEL '${s[2]}'"
done

gtidstr=$(IFS=,; echo "${gtids[*]}")
echo "gtids: $gtidstr"
mysql -u root -e "RESET MASTER; SET GLOBAL gtid_purged='$gtidstr'; START SLAVE"
