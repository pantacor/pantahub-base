//
// Copyright 2018  Pantacor Ltd.
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
	"strconv"

	"github.com/justincampbell/timeago"
	"github.com/olekukonko/tablewriter"
	"github.com/urfave/cli"
	"gitlab.com/pantacor/pvr/libpvr"
)

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}
func CommandPs() cli.Command {
	return cli.Command{
		Name:        "ps",
		Aliases:     []string{"ps"},
		Usage:       "Show Owned Devices",
		Description: "Get a quick overview of devices you manage in Pantahub",
		Action: func(c *cli.Context) error {

			session, err := libpvr.NewSession(c.App)

			if err != nil {
				return cli.NewExitError(err, 4)
			}

			devices, err := session.DoPs(c.App.Metadata["PVR_BASEURL"].(string))

			if err != nil {
				return cli.NewExitError("Error getting device list: "+err.Error(), 4)
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetBorder(false)
			table.SetHeaderLine(false)
			table.SetColumnSeparator(" ")
			table.SetHeader([]string{"id", "nick", "rev", "status", "state", "seen", "ip", "message"})

			for _, v := range devices {
				table.Append([]string{
					v.Id[:8],
					v.Nick,
					strconv.Itoa(v.ProgressRevision),
					v.Status,
					v.StateSha[:min(len(v.StateSha), 8)],
					timeago.FromTime(v.Timestamp),
					v.RealIP,
					v.StatusMsg})
			}

			table.Render()

			return nil
		},
	}
}
