//
// Copyright 2017  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//
package main

import (
	"errors"
	"os"

	"github.com/urfave/cli"
)

func CommandGet() cli.Command {
	return cli.Command{
		Name:        "get",
		Aliases:     []string{"g"},
		ArgsUsage:   "[repository [target-repository]]",
		Usage:       "get update target-repository from repository",
		Description: "default target-repository is the local .pvr one. If not <repository> is provided the last one is used.",
		Action: func(c *cli.Context) error {
			wd, err := os.Getwd()
			if err != nil {
				return cli.NewExitError(err, 1)
			}

			pvr, err := NewPvr(c.App, wd)
			if err != nil {
				return cli.NewExitError(err, 2)
			}

			var repoPath string

			if c.NArg() > 1 {
				return errors.New("Get can have at most 1 argument. See --help.")
			} else if c.NArg() == 0 {
				repoPath = ""
			} else {
				repoPath = c.Args()[0]
			}

			err = pvr.GetRepo(repoPath)
			if err != nil {
				return cli.NewExitError(err, 3)
			}

			return nil
		},
	}
}
