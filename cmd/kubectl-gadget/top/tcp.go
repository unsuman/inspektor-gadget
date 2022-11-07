// Copyright 2019-2021 The Inspektor Gadget authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package top

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/spf13/cobra"

	commonutils "github.com/inspektor-gadget/inspektor-gadget/cmd/common/utils"
	"github.com/inspektor-gadget/inspektor-gadget/cmd/kubectl-gadget/utils"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/columns"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/gadgets/top"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/gadgets/top/tcp/types"
)

type TCPParser struct {
	commonutils.BaseParser[types.Stats]
	sync.Mutex

	flags     *CommonTopFlags
	nodeStats map[string][]*types.Stats
	colMap    columns.ColumnMap[types.Stats]
}

func newTCPCmd() *cobra.Command {
	commonTopFlags := &CommonTopFlags{
		CommonFlags: utils.CommonFlags{
			OutputConfig: commonutils.OutputConfig{
				// The columns that will be used in case the user does not specify
				// which specific columns they want to print.
				CustomColumns: []string{
					"node",
					"namespace",
					"pod",
					"container",
					"pid",
					"comm",
					"ip",
					"saddr",
					"daddr",
					"sent",
					"received",
				},
			},
		},
	}

	var (
		filteredPid uint
		family      uint
	)

	columnsWidth := map[string]int{
		"node":      -16,
		"namespace": -16,
		"pod":       -30,
		"container": -16,
		"pid":       -7,
		"comm":      -16,
		"ip":        -3,
		"saddr":     -51,
		"daddr":     -51,
		"sent":      -7,
		"received":  -7,
	}

	cols := columns.MustCreateColumns[types.Stats]()

	cmd := &cobra.Command{
		Use:   fmt.Sprintf("tcp [interval=%d]", top.IntervalDefault),
		Short: "Periodically report TCP activity",
		RunE: func(cmd *cobra.Command, args []string) error {
			parser := &TCPParser{
				BaseParser: commonutils.NewBaseWidthParser[types.Stats](columnsWidth, &commonTopFlags.OutputConfig),
				flags:      commonTopFlags,
				nodeStats:  make(map[string][]*types.Stats),
			}

			parser.colMap = cols.GetColumnMap()

			parameters := make(map[string]string)
			if family != 0 {
				parameters[types.FamilyParam] = strconv.FormatUint(uint64(family), 10)
			}
			if filteredPid != 0 {
				parameters[types.PidParam] = strconv.FormatUint(uint64(filteredPid), 10)
			}

			gadget := &TopGadget[types.Stats]{
				name:           "tcptop",
				commonTopFlags: commonTopFlags,
				params:         parameters,
				parser:         parser,
			}

			return gadget.Run(args)
		},
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
	}

	addCommonTopFlags(cmd, commonTopFlags, &commonTopFlags.CommonFlags, cols.GetColumnNames(), types.SortByDefault)

	cmd.PersistentFlags().UintVarP(
		&filteredPid,
		"pid",
		"",
		0,
		"Show only TCP events generated by this particular PID",
	)
	cmd.PersistentFlags().UintVarP(
		&family,
		"family",
		"f",
		0,
		"Show only TCP events for this IP version: either 4 or 6 (by default all will be printed)",
	)

	return cmd
}

func (p *TCPParser) Callback(line string, node string) {
	p.Lock()
	defer p.Unlock()

	var event top.Event[types.Stats]

	if err := json.Unmarshal([]byte(line), &event); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s", commonutils.WrapInErrUnmarshalOutput(err, line))
		return
	}

	if event.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: failed on node %q: %s", node, event.Error)
		return
	}

	p.nodeStats[node] = event.Stats
}

func (p *TCPParser) PrintStats() {
	// Sort and print stats
	p.Lock()

	stats := []*types.Stats{}
	for _, stat := range p.nodeStats {
		stats = append(stats, stat...)
	}
	p.nodeStats = make(map[string][]*types.Stats)

	p.Unlock()

	top.SortStats(stats, p.flags.ParsedSortBy, &p.colMap)

	for idx, stat := range stats {
		if idx == p.flags.MaxRows {
			break
		}
		fmt.Println(p.TransformStats(stat))
	}
}

func (p *TCPParser) TransformStats(stats *types.Stats) string {
	return p.Transform(stats, func(stats *types.Stats) string {
		var sb strings.Builder

		for _, col := range p.OutputConfig.CustomColumns {
			switch col {
			case "node":
				sb.WriteString(fmt.Sprintf("%*s", p.ColumnsWidth[col], stats.Node))
			case "namespace":
				sb.WriteString(fmt.Sprintf("%*s", p.ColumnsWidth[col], stats.Namespace))
			case "pod":
				sb.WriteString(fmt.Sprintf("%*s", p.ColumnsWidth[col], stats.Pod))
			case "container":
				sb.WriteString(fmt.Sprintf("%*s", p.ColumnsWidth[col], stats.Container))
			case "pid":
				sb.WriteString(fmt.Sprintf("%*d", p.ColumnsWidth[col], stats.Pid))
			case "comm":
				sb.WriteString(fmt.Sprintf("%*s", p.ColumnsWidth[col], stats.Comm))
			case "ip":
				tcpFamily := 4
				if stats.Family == syscall.AF_INET6 {
					tcpFamily = 6
				}

				sb.WriteString(fmt.Sprintf("%*d", p.ColumnsWidth[col], tcpFamily))
			case "saddr":
				sb.WriteString(fmt.Sprintf("%*s", p.ColumnsWidth[col], fmt.Sprintf("%s:%d", stats.Saddr, stats.Sport)))
			case "daddr":
				sb.WriteString(fmt.Sprintf("%*s", p.ColumnsWidth[col], fmt.Sprintf("%s:%d", stats.Daddr, stats.Dport)))
			case "sent":
				sb.WriteString(fmt.Sprintf("%*d", p.ColumnsWidth[col], stats.Sent/1024))
			case "received":
				sb.WriteString(fmt.Sprintf("%*d", p.ColumnsWidth[col], stats.Received/1024))
			}
			sb.WriteRune(' ')
		}

		return sb.String()
	})
}
