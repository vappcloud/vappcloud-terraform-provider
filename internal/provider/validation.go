package provider

import "regexp"

func apiURLPattern() *regexp.Regexp {
	return regexp.MustCompile(`^https?://[^\s/$.?#].[^\s]*$`)
}
