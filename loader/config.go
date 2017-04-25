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
	"flag"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/juju/errors"
)

// NewConfig creates a new config.
func NewConfig() *Config {
	cfg := &Config{}
	cfg.FlagSet = flag.NewFlagSet("loader", flag.ContinueOnError)
	fs := cfg.FlagSet

	fs.StringVar(&cfg.Dir, "d", "./", "Directory of the dump to import")

	fs.IntVar(&cfg.PoolSize, "t", 4, "Number of threads for each pool")
	fs.IntVar(&cfg.PoolCount, "pc", 16, `Number of pools restore concurrently, one pool restore one block
	at a time, increase this as TiKV nodes increase`)
	fs.IntVar(&cfg.FileNumPerBlock, "file-num-per-block", 64, `Number of data files per block`)

	fs.StringVar(&cfg.DB.Host, "h", "127.0.0.1", "The host to connect to")
	fs.StringVar(&cfg.DB.User, "u", "root", "Username with privileges to run the dump")
	fs.StringVar(&cfg.DB.Password, "p", "", "User password")
	fs.IntVar(&cfg.DB.Port, "P", 4000, "TCP/IP port to connect to")
	fs.IntVar(&cfg.SkipConstraintCheck, "skip-unique-check", 1, "Skip constraint check")

	fs.StringVar(&cfg.CheckPoint, "checkpoint", "loader.checkpoint", "Store files that has restored")

	fs.StringVar(&cfg.PprofAddr, "pprof-addr", ":10084", "Loader pprof addr")
	fs.StringVar(&cfg.LogLevel, "L", "info", "Loader log level: debug, info, warn, error, fatal")

	fs.StringVar(&cfg.configFile, "c", "", "config file")

	return cfg
}

// DBConfig is the DB configuration.
type DBConfig struct {
	Host string `toml:"host" json:"host"`

	User string `toml:"user" json:"user"`

	Password string `toml:"password" json:"password"`

	Port int `toml:"port" json:"port"`
}

func (c *DBConfig) String() string {
	if c == nil {
		return "<nil>"
	}
	return fmt.Sprintf("DBConfig(%+v)", *c)
}

// Config is the configuration.
type Config struct {
	*flag.FlagSet `json:"-"`

	LogLevel string `toml:"log-level" json:"log-level"`

	LogFile string `toml:"log-file" json:"log-file"`

	PprofAddr string `toml:"pprof-addr" json:"pprof-addr"`

	FileNumPerBlock int `toml:"file-num-per-block" json:"file-num-per-block"`

	PoolSize int `toml:"pool-size" json:"pool-size"`

	PoolCount int `toml:"pool-count" json:"pool-count"`

	Dir string `toml:"dir" json:"dir"`

	CheckPoint string `toml:"checkpoint" json:"checkpoint"`

	DB DBConfig `toml:"db" json:"db"`

	configFile          string
	SkipConstraintCheck int `toml:"skip-unique-check" json:"skip-unique-check"`

	RouteRules []*RouteRule `toml:"route-rules" json:"route-rules"`
}

// RouteRule is the route rule for loading schema and table into specified schema and table.
type RouteRule struct {
	SchemaPattern string `toml:"schema-pattern" json:"schema-pattern"`
	TablePattern  string `toml:"table-pattern" json:"table-pattern"`
	TargetSchema  string `toml:"target-schema" json:"target-schema"`
	TargetTable   string `toml:"target-table" json:"target-table"`
}

// Parse parses flag definitions from the argument list.
func (c *Config) Parse(arguments []string) error {
	// Parse first to get config file.
	err := c.FlagSet.Parse(arguments)
	if err != nil {
		return errors.Trace(err)
	}

	// Load config file if specified.
	if c.configFile != "" {
		err = c.configFromFile(c.configFile)
		if err != nil {
			return errors.Trace(err)
		}
	}

	// Parse again to replace with command line options.
	err = c.FlagSet.Parse(arguments)
	if err != nil {
		return errors.Trace(err)
	}

	if len(c.FlagSet.Args()) != 0 {
		return errors.Errorf("'%s' is an invalid flag", c.FlagSet.Arg(0))
	}

	return nil
}

func (c *Config) String() string {
	if c == nil {
		return "<nil>"
	}
	return fmt.Sprintf("Config(%+v)", *c)
}

// configFromFile loads config from file.
func (c *Config) configFromFile(path string) error {
	_, err := toml.DecodeFile(path, c)
	return errors.Trace(err)
}
