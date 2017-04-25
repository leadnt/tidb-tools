// Copyright 2016 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/juju/errors"
	"github.com/ngaut/log"
	"github.com/pingcap/tidb-tools/pkg/tableroute"
	tmysql "github.com/pingcap/tidb/mysql"
)

// Conn represents a live DB connection
type Conn struct {
	db *sql.DB
}

func querySQL(db *sql.DB, query string) (*sql.Rows, error) {
	var (
		err  error
		rows *sql.Rows
	)

	log.Debugf("[query][sql]%s", query)

	rows, err = db.Query(query)
	if err != nil {
		log.Errorf("query sql[%s] failed %v", query, errors.ErrorStack(err))
		return nil, errors.Trace(err)
	}

	return rows, nil
}

func executeSQL(conn *Conn, sqls []string, enableRetry bool, skipConstraintCheck bool) error {
	if len(sqls) == 0 {
		return nil
	}

	var err error

	retryCount := 1
	if enableRetry {
		retryCount = maxRetryCount
	}

	for i := 0; i < retryCount; i++ {
		if i > 0 {
			log.Warnf("exec sql retry %d - %-.100v", i, sqls)
			time.Sleep(2 * time.Duration(i) * time.Second)
		}

		// retry can not skip constraint check.
		if i > 0 {
			skipConstraintCheck = false
		}
		if err = executeSQLImp(conn.db, sqls, skipConstraintCheck); err != nil {
			if !isErrDupEntry(err) {
				continue
			}
		}

		return nil
	}

	return errors.Trace(err)
}

func executeSQLImp(db *sql.DB, sqls []string, skipConstraintCheck bool) error {
	var (
		err error
		txn *sql.Tx
	)

	txn, err = db.Begin()
	if err != nil {
		log.Errorf("exec sqls[%-.100v] begin failed %v", sqls, errors.ErrorStack(err))
		return err
	}

	// If the database has a concept of per-connection state, such state can only be reliably
	// observed within a transaction.
	if skipConstraintCheck {
		_, err = txn.Exec("set @@session.tidb_skip_constraint_check=1;")
	} else {
		_, err = txn.Exec("set @@session.tidb_skip_constraint_check=0;")
	}
	if err != nil {
		log.Errorf("exec set session.tidb_skip_constraint_check failed %v", errors.ErrorStack(err))
		return err
	}

	for i := range sqls {
		log.Debugf("[exec][sql]%-.200v", sqls)

		_, err = txn.Exec(sqls[i])
		if err != nil {
			log.Warnf("[exec][sql]%-.100v[error]%v", sqls, err)
			rerr := txn.Rollback()
			if rerr != nil {
				log.Errorf("[exec][sql]%-.100s[error]%v", sqls, rerr)
			}
			return err
		}
	}

	err = txn.Commit()
	if err != nil {
		log.Errorf("exec sqls[%-.100v] commit failed %v", sqls, errors.ErrorStack(err))
		return err
	}

	return nil
}

func hasUniqIndex(conn *Conn, schema string, table string, tableRouter route.TableRouter) (bool, error) {
	if schema == "" || table == "" {
		return false, errors.New("schema/table is empty")
	}

	targetSchema, targetTable := fetchMatchedLiteral(tableRouter, schema, table)

	query := fmt.Sprintf("show index from %s.%s", targetSchema, targetTable)
	rows, err := querySQL(conn.db, query)
	if err != nil {
		return false, errors.Trace(err)
	}
	defer rows.Close()

	rowColumns, err := rows.Columns()
	if err != nil {
		return false, errors.Trace(err)
	}

	// Show an example.
	/*
		mysql> show index from test.t;
		+-------+------------+----------+--------------+-------------+-----------+-------------+----------+--------+------+------------+---------+---------------+
		| Table | Non_unique | Key_name | Seq_in_index | Column_name | Collation | Cardinality | Sub_part | Packed | Null | Index_type | Comment | Index_comment |
		+-------+------------+----------+--------------+-------------+-----------+-------------+----------+--------+------+------------+---------+---------------+
		| t     |          0 | PRIMARY  |            1 | a           | A         |           0 |     NULL | NULL   |      | BTREE      |         |               |
		| t     |          0 | PRIMARY  |            2 | b           | A         |           0 |     NULL | NULL   |      | BTREE      |         |               |
		| t     |          0 | ucd      |            1 | c           | A         |           0 |     NULL | NULL   | YES  | BTREE      |         |               |
		| t     |          0 | ucd      |            2 | d           | A         |           0 |     NULL | NULL   | YES  | BTREE      |         |               |
		+-------+------------+----------+--------------+-------------+-----------+-------------+----------+--------+------+------------+---------+---------------+
	*/

	for rows.Next() {
		datas := make([]sql.RawBytes, len(rowColumns))
		values := make([]interface{}, len(rowColumns))

		for i := range values {
			values[i] = &datas[i]
		}

		err = rows.Scan(values...)
		if err != nil {
			return false, errors.Trace(err)
		}

		nonUnique := string(datas[1])
		if nonUnique == "0" {
			return true, nil
		}
	}

	if rows.Err() != nil {
		return false, errors.Trace(rows.Err())
	}

	return false, nil
}

func truncateTable(conn *Conn, schema string, table string) error {
	if schema == "" || table == "" {
		return errors.New("schema/table is empty")
	}

	query := fmt.Sprintf("truncate table `%s`.`%s`;", schema, table)
	rows, err := querySQL(conn.db, query)
	if err != nil {
		return errors.Trace(err)
	}
	defer rows.Close()

	log.Info(query)

	return nil
}

func createConn(cfg DBConfig) (*Conn, error) {
	dbDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	db, err := sql.Open("mysql", dbDSN)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return &Conn{db: db}, nil
}

func closeConn(conn *Conn) error {
	if conn.db == nil {
		return nil
	}

	return errors.Trace(conn.db.Close())
}

func createConns(cfg DBConfig, count int) ([]*Conn, error) {
	conns := make([]*Conn, 0, count)
	for i := 0; i < count; i++ {
		conn, err := createConn(cfg)
		if err != nil {
			return nil, errors.Trace(err)
		}

		conns = append(conns, conn)
	}

	return conns, nil
}

func closeConns(conns ...*Conn) {
	for _, conn := range conns {
		err := closeConn(conn)
		if err != nil {
			log.Errorf("close db failed - %v", err)
		}
	}
}

func isErrDBExists(err error) bool {
	err = causeErr(err)
	if e, ok := err.(*mysql.MySQLError); ok && e.Number == tmysql.ErrDBCreateExists {
		return true
	}
	return false
}

func isErrTableExists(err error) bool {
	err = causeErr(err)
	if e, ok := err.(*mysql.MySQLError); ok && e.Number == tmysql.ErrTableExists {
		return true
	}
	return false
}

func isErrDupEntry(err error) bool {
	err = causeErr(err)
	if e, ok := err.(*mysql.MySQLError); ok && e.Number == tmysql.ErrDupEntry {
		return true
	}
	return false
}
