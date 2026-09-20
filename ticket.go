package main

// Ticket keys in branch names, titles and folder names.
//
// This file is the same in every tool of the family that needs it.

import (
	"regexp"
	"strings"
)

// ticketRe matches a Jira-style ticket key: a project key of 2+ letters, a
// dash and digits (FED-2030, plat-1193). It is not anchored to word
// boundaries: an underscore is a word character, and fix_eshop-270 holds a
// ticket.
var ticketRe = regexp.MustCompile(`[A-Za-z]{2,}-[0-9]+`)

// ticketFrom extracts a normalized (uppercase) ticket key from the first
// source that contains one. Returns "" when none matches.
func ticketFrom(sources ...string) string {
	for _, s := range sources {
		if m := ticketRe.FindString(s); m != "" {
			return strings.ToUpper(m)
		}
	}
	return ""
}
