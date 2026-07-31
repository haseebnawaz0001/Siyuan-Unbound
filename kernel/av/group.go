// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package av

// ViewGroup describes the structure of a view's grouping rule.
type ViewGroup struct {
	Field     string      `json:"field"`           // ID of the field to group by
	Method    GroupMethod `json:"method"`          // Grouping method
	Range     *GroupRange `json:"range,omitempty"` // Grouping range
	Order     GroupOrder  `json:"order"`           // Group sort order
	HideEmpty bool        `json:"hideEmpty"`       // Whether to hide empty groups
}

// GroupMethod describes the grouping method.
type GroupMethod int

const (
	GroupMethodValue        GroupMethod = iota // Group by value
	GroupMethodRangeNum                        // Group by numeric range
	GroupMethodDateRelative                    // Group by relative date
	GroupMethodDateDay                         // Group by day
	GroupMethodDateWeek                        // Group by week
	GroupMethodDateMonth                       // Group by month
	GroupMethodDateYear                        // Group by year
)

// GroupRange describes the structure of a grouping range.
type GroupRange struct {
	NumStart float64 `json:"numStart"` // Start value of the numeric range
	NumEnd   float64 `json:"numEnd"`   // End value of the numeric range
	NumStep  float64 `json:"numStep"`  // Step size of the numeric range
}

// GroupOrder describes the group sort order.
type GroupOrder int

const (
	GroupOrderAsc          = iota // Ascending order
	GroupOrderDesc                // Descending order
	GroupOrderMan                 // Manual order
	GroupOrderSelectOption        // Follow the select option order (applies only to single-select and multi-select fields) https://github.com/siyuan-note/siyuan/issues/15500
)
