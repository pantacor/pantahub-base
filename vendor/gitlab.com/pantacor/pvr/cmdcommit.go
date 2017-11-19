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
	"os"

	"github.com/urfave/cli"
)

func CommandCommit() cli.Command {
	return cli.Command{
		Name:      "commit",
		Aliases:   []string{"ci"},
		ArgsUsage: "",
		Usage:     "commit status changes",
		Action: func(c *cli.Context) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}

			pvr, err := NewPvr(c.App, wd)
			if err != nil {
				return err
			}

			commitmsg := c.String("message")
			if commitmsg == "" {
				commitmsg = "** No commit message **"
			}
			err = pvr.Commit(commitmsg)
			if err != nil {
				return err
			}

			return nil
		},
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "message, m",
				Usage: "provide a commit message",
			},
		},
	}
}
