package utils

import (
	"strings"
	"regexp"
)

// StringInListCaseInsensitive return true if str is in the list (case insensitive)
func StringInListCaseInsensitive(list []string, str string) bool {
	for _, s := range list {
		if strings.ToLower(s) == strings.ToLower(str) {
			return true
		}
	}
	return false
}

// StringListRegexpMatch return true if str is matching any of the regexp in the list
func StringListRegexpMatch(list []*regexp.Regexp, str string) bool {
	for _, r := range list {
		if r.MatchString(strings.ToLower(str)) {
			return true
		}
	}
	return false
}
