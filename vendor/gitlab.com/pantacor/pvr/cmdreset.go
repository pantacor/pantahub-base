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

func CommandReset() cli.Command {
	return cli.Command{
		Name:        "reset",
		Aliases:     []string{"r", "checkout", "co"},
		ArgsUsage:   "",
		Usage:       "reset working directory to match the repo state",
		Description: "reset/checkout also forgets about added files; pvr status and diff will yield empty",
		Action: func(c *cli.Context) error {
			wd, err := os.Getwd()
			if err != nil {
				return cli.NewExitError(err, 1)
			}

			pvr, err := NewPvr(c.App, wd)
			if err != nil {
				return cli.NewExitError(err, 2)
			}

			err = pvr.Reset()
			if err != nil {
				return cli.NewExitError(err, 3)
			}

			return nil
		},
	}
}
