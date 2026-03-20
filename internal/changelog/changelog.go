package changelog

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ChangeLogChange struct {
	Type string
	Text string
}

type ChangelogEntry struct {
	AbsSourceFile string
	Date          time.Time
	Title         string
	MajorChanges  []ChangeLogChange
	MinorChanges  []ChangeLogChange
	Labels        []string
	Links         []string
	Authors       []string
}

type SortRule struct {
	IsDate bool
	Order  string
	Label  string
}

type ChangelogRecord struct {
	SourcePatterns []string
	TargetFiles    []string
	IncludeLabels  []string
	ExcludeLabels  []string
	SortRules      []SortRule
}

type ChangelogConfig struct {
	Records []ChangelogRecord
}

func parseDate(yDate interface{}) (time.Time, error) {
	switch v := yDate.(type) {
	case time.Time:
		return v, nil
	case string:
		// Attempt YYYY-MM-DD
		if t, err := time.Parse("2006-01-02", v); err == nil {
			return t, nil
		}
		// Attempt DD-MM-YYYY
		if t, err := time.Parse("02-01-2006", v); err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("invalid date format: %v", v)
	default:
		return time.Time{}, fmt.Errorf("unknown date type: %T", yDate)
	}
}

func parseChanges(yData interface{}) []ChangeLogChange {
	var results []ChangeLogChange
	list, ok := yData.([]interface{})
	if !ok {
		return results
	}
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		for k, v := range m {
			results = append(results, ChangeLogChange{
				Type: strings.ToLower(k),
				Text: fmt.Sprintf("%v", v),
			})
			break // System expects 1 key per entry
		}
	}
	return results
}

func (c *ChangelogConfig) LoadEntries(projectDir string) ([]ChangelogEntry, error) {
	var allEntries []ChangelogEntry
	seen := make(map[string]bool)

	for _, record := range c.Records {
		for _, pattern := range record.SourcePatterns {
			fullPattern := filepath.Join(projectDir, pattern)
			matches, err := filepath.Glob(fullPattern)
			if err != nil {
				continue
			}

			for _, match := range matches {
				if seen[match] {
					continue
				}
				seen[match] = true

				data, err := ioutil.ReadFile(match)
				if err != nil {
					continue
				}

				var yMap map[string]interface{}
				if err := yaml.Unmarshal(data, &yMap); err != nil {
					continue
				}

				date, _ := parseDate(yMap["date"])
				title, _ := yMap["title"].(string)

				entry := ChangelogEntry{
					AbsSourceFile: match,
					Date:          date,
					Title:         title,
					MajorChanges:  parseChanges(yMap["changes"]),
					MinorChanges:  parseChanges(yMap["subchanges"]),
				}

				if lbs, ok := yMap["labels"].([]interface{}); ok {
					for _, lb := range lbs {
						entry.Labels = append(entry.Labels, fmt.Sprintf("%v", lb))
					}
				}
				if lnks, ok := yMap["links"].([]interface{}); ok {
					for _, lnk := range lnks {
						entry.Links = append(entry.Links, fmt.Sprintf("%v", lnk))
					}
				}
				if auths, ok := yMap["authors"].([]interface{}); ok {
					for _, auth := range auths {
						entry.Authors = append(entry.Authors, fmt.Sprintf("%v", auth))
					}
				}

				// Filtering
				if len(record.IncludeLabels) > 0 {
					matchFound := false
					for _, l := range entry.Labels {
						for _, inc := range record.IncludeLabels {
							if strings.EqualFold(l, inc) {
								matchFound = true
								break
							}
						}
					}
					if !matchFound {
						continue
					}
				}

				if len(record.ExcludeLabels) > 0 {
					excludeFound := false
					for _, l := range entry.Labels {
						for _, exc := range record.ExcludeLabels {
							if strings.EqualFold(l, exc) {
								excludeFound = true
								break
							}
						}
					}
					if excludeFound {
						continue
					}
				}

				allEntries = append(allEntries, entry)
			}
		}
	}
	return allEntries, nil
}

func ipow(base, exp int) int {
	res := 1
	for exp > 0 {
		res *= base
		exp--
	}
	return res
}

func SortEntries(entries []ChangelogEntry, rules []SortRule) {
	weight := len(rules)
	sort.Slice(entries, func(i, j int) bool {
		val := 0
		for idx, rule := range rules {
			greater := false
			less := false
			if rule.IsDate {
				if rule.Order == "ascending" || rule.Order == "asc" {
					less = entries[i].Date.Before(entries[j].Date)
					greater = entries[i].Date.After(entries[j].Date)
				} else {
					less = entries[i].Date.After(entries[j].Date)
					greater = entries[i].Date.Before(entries[j].Date)
				}
			} else if rule.Label != "" {
				iHas := false
				for _, l := range entries[i].Labels {
					if strings.EqualFold(l, rule.Label) {
						iHas = true
						break
					}
				}
				jHas := false
				for _, l := range entries[j].Labels {
					if strings.EqualFold(l, rule.Label) {
						jHas = true
						break
					}
				}
				less = iHas && !jHas
				greater = !iHas && jHas
			}

			w := weight - idx
			p := ipow(w, w)
			if less {
				val -= p
			} else if greater {
				val += p
			}
		}
		return val < 0
	})
}

func GenerateMarkdown(entries []ChangelogEntry, target string) error {
	if len(entries) == 0 {
		return nil
	}

	var sb strings.Builder
	fileTitle := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	
	sb.WriteString(fmt.Sprintf("# Change Log '%s'\n\n", fileTitle))
	sb.WriteString("This document was auto generated by [Go Mod Builder](https://github.com/Polypheides/GoModBuilder). Do not edit by hand.\n\n")

	// Summary Statistics (Simplified port)
	majorCounts := make(map[string]int)
	minorCounts := make(map[string]int)
	labelCounts := make(map[string]int)
	
	for _, entry := range entries {
		for _, c := range entry.MajorChanges {
			majorCounts[c.Type]++
		}
		for _, c := range entry.MinorChanges {
			minorCounts[c.Type]++
		}
		for _, l := range entry.Labels {
			labelCounts[l]++
		}
	}

	sb.WriteString("### Summary\n")
	sb.WriteString(fmt.Sprintf("Contains %d entries with:\n", len(entries)))
	for t, c := range majorCounts {
		sb.WriteString(fmt.Sprintf("- %s (%d)\n", strings.ToUpper(t), c))
	}
	sb.WriteString("\n")

	// Index
	sb.WriteString("### Index\n")
	for i, entry := range entries {
		dateStr := entry.Date.Format("2006-01-02")
		anchor := fmt.Sprintf("link__%s__%s", entry.Date.Format("20060102"), strings.ToLower(strings.ReplaceAll(filepath.Base(entry.AbsSourceFile), " ", "")))
		sb.WriteString(fmt.Sprintf("%d. [%s - %s](#%s)\n", i+1, dateStr, entry.Title, anchor))
	}
	sb.WriteString("\n")

	// Details
	sb.WriteString("### Details\n")
	for _, entry := range entries {
		dateStr := entry.Date.Format("2006-01-02")
		anchor := fmt.Sprintf("link__%s__%s", entry.Date.Format("20060102"), strings.ToLower(strings.ReplaceAll(filepath.Base(entry.AbsSourceFile), " ", "")))
		
		sb.WriteString("---\n")
		sb.WriteString(fmt.Sprintf("#### %s - %s <a name='%s'></a>\n\n", dateStr, entry.Title, anchor))
		
		if len(entry.MajorChanges) > 0 {
			sb.WriteString("**Changes**\n")
			for _, c := range entry.MajorChanges {
				sb.WriteString(fmt.Sprintf("- **%s**: %s\n", strings.ToUpper(c.Type), c.Text))
			}
			sb.WriteString("\n")
		}

		if len(entry.MinorChanges) > 0 {
			sb.WriteString("**Subchanges**\n")
			for _, c := range entry.MinorChanges {
				sb.WriteString(fmt.Sprintf("- **%s**: %s\n", strings.ToUpper(c.Type), c.Text))
			}
			sb.WriteString("\n")
		}

		if len(entry.Links) > 0 {
			sb.WriteString("**Links**\n")
			for _, l := range entry.Links {
				sb.WriteString(fmt.Sprintf("- [%s](%s)\n", l, l))
			}
			sb.WriteString("\n")
		}

		if len(entry.Labels) > 0 {
			sb.WriteString(fmt.Sprintf("**Labels:** %s  \n", strings.Join(entry.Labels, ", ")))
		}
		if len(entry.Authors) > 0 {
			sb.WriteString(fmt.Sprintf("**Authors:** %s  \n", strings.Join(entry.Authors, ", ")))
		}
		sb.WriteString(fmt.Sprintf("**Source:** %s\n\n", filepath.Base(entry.AbsSourceFile)))
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(target, []byte(sb.String()), 0644)
}
