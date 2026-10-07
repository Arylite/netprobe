package auth

import (
	"strings"
	"unicode/utf8"
)

// minDistinct is how many different characters a password needs: "aaaaaaaaaaaa"
// is twelve characters long and no password at all.
const minDistinct = 5

// common are passwords that are long enough for the length rule and are the
// first ones anyone tries, in lower case. A password is also refused when it is
// one of them followed by digits or punctuation: "Password1234!" is "password".
var common = map[string]bool{}

func init() {
	for _, p := range []string{
		"password", "passw0rd", "password123", "passwordpassword", "administrator", "administrateur",
		"qwertyuiop", "qwertyuiopas", "qwertyuiopasdf", "azertyuiop", "azertyuiopqs", "azertyuiopqsdf",
		"asdfghjkl", "zxcvbnm", "qazwsxedc", "1qaz2wsx3edc", "iloveyou", "welcome", "welcome123",
		"letmeinplease", "letmein", "changeme", "changemenow", "trustno1", "football", "baseball",
		"superman", "batman", "monkey", "dragon", "sunshine", "princess", "starwars", "master",
		"shadow", "mustang", "michael", "jennifer", "whatever", "freedom", "secret", "secretsecret",
		"adminadmin", "adminpassword", "rootroot", "toor", "default", "defaultpassword", "netprobe",
		"netprobepassword", "monitoring", "supervision", "motdepasse", "bonjour", "soleil", "doudou",
		"123456789012", "1234567890123", "12345678901234", "111111111111", "000000000000",
		"abcdefghijkl", "abcdefghijklm", "abcd1234abcd", "password1234", "password12345",
		"p@ssw0rd", "p@ssword", "pa$$word", "passwordpassword1", "letmein123", "iloveyou123",
	} {
		common[p] = true
	}
}

// weak reports why a password is too easy to guess, whatever its length.
func weak(password string) string {
	lower := strings.ToLower(password)
	stem := strings.TrimRight(lower, "0123456789!@#$%^&*()-_=+.?,;:/")
	switch {
	case common[lower] || (stem != "" && common[stem]):
		return "the password is one of the most common ones"
	case distinct(password) < minDistinct:
		return "the password repeats the same few characters"
	case sequential(password):
		return "the password is a sequence of characters"
	}
	return ""
}

func distinct(s string) int {
	seen := map[rune]bool{}
	for _, r := range s {
		seen[r] = true
	}
	return len(seen)
}

// sequential reports a string whose characters each follow the one before, up or
// down: "abcdefghijkl", "987654321098".
func sequential(s string) bool {
	runes := []rune(s)
	if utf8.RuneCountInString(s) < 2 {
		return false
	}
	up, down := true, true
	for i := 1; i < len(runes); i++ {
		d := runes[i] - runes[i-1]
		// A digit sequence wraps: 9 is followed by 0.
		if d != 1 && (runes[i-1] != '9' || runes[i] != '0') {
			up = false
		}
		if d != -1 && (runes[i-1] != '0' || runes[i] != '9') {
			down = false
		}
	}
	return up || down
}
