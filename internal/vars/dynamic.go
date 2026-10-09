package vars

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	firstNames = []string{"Ada", "Alan", "Grace", "Linus", "Margaret", "Dennis", "Barbara", "Ken", "Frances", "Edsger", "Radia", "Tim", "Katherine", "John", "Hedy", "Niklaus"}
	lastNames  = []string{"Lovelace", "Turing", "Hopper", "Torvalds", "Hamilton", "Ritchie", "Liskov", "Thompson", "Allen", "Dijkstra", "Perlman", "Berners-Lee", "Johnson", "McCarthy", "Lamarr", "Wirth"}
	cities     = []string{"Lisbon", "Kyiv", "Oslo", "Tokyo", "Toronto", "Nairobi", "Lima", "Seoul", "Prague", "Austin", "Dublin", "Melbourne"}
	countries  = []string{"Portugal", "Ukraine", "Norway", "Japan", "Canada", "Kenya", "Peru", "Korea", "Czechia", "United States", "Ireland", "Australia"}
	ccodes     = []string{"PT", "UA", "NO", "JP", "CA", "KE", "PE", "KR", "CZ", "US", "IE", "AU"}
	streets    = []string{"Main St", "Oak Avenue", "Maple Road", "Elm Street", "Pine Lane", "Cedar Court", "Birch Way"}
	words      = []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa", "quebec", "romeo", "sierra", "tango"}
	lorem      = strings.Fields("lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua")
	nouns      = []string{"bandwidth", "protocol", "array", "pixel", "driver", "matrix", "circuit", "monitor", "panel", "card", "system", "feed"}
	verbs      = []string{"parse", "compress", "index", "override", "transmit", "navigate", "generate", "bypass", "connect", "calculate", "quantify", "program"}
	adjectives = []string{"optical", "virtual", "digital", "primary", "solid state", "redundant", "neural", "wireless", "bluetooth", "haptic", "mobile", "cross-platform"}
	colors     = []string{"red", "green", "blue", "orange", "purple", "teal", "yellow", "black", "white", "magenta"}
	currencies = []string{"USD", "EUR", "GBP", "UAH", "JPY", "CAD", "CHF", "AUD"}
	jobs       = []string{"Engineer", "Designer", "Architect", "Analyst", "Manager", "Consultant", "Developer", "Administrator"}
	companies  = []string{"Initech", "Globex", "Hooli", "Acme", "Umbrella", "Stark Industries", "Wayne Enterprises", "Wonka"}
	tlds       = []string{"com", "net", "org", "io", "dev", "info"}
	locales    = []string{"en", "uk", "de", "fr", "es", "pt", "ja", "it"}
)

func pick(list []string) string { return list[rand.IntN(len(list))] }

func alnum(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.IntN(len(chars))]
	}
	return string(b)
}

func randomDate(offsetDays int) string {
	d := time.Duration(rand.IntN(offsetDays*24*3600)) * time.Second
	if offsetDays < 0 {
		d = -time.Duration(rand.IntN(-offsetDays*24*3600)) * time.Second
	}
	return time.Now().Add(d).UTC().Format(time.RFC1123)
}

var dynamic = map[string]func() string{
	"$guid":        uuid.NewString,
	"$randomUUID":  uuid.NewString,
	"$timestamp":   func() string { return strconv.FormatInt(time.Now().Unix(), 10) },
	"$isoTimestamp": func() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") },
	"$randomInt":   func() string { return strconv.Itoa(rand.IntN(1001)) },
	"$randomBoolean": func() string {
		return strconv.FormatBool(rand.IntN(2) == 1)
	},
	"$randomAlphaNumeric": func() string { return alnum(1) },
	"$randomFirstName":    func() string { return pick(firstNames) },
	"$randomLastName":     func() string { return pick(lastNames) },
	"$randomFullName":     func() string { return pick(firstNames) + " " + pick(lastNames) },
	"$randomUserName":     func() string { return strings.ToLower(pick(firstNames)) + strconv.Itoa(rand.IntN(100)) },
	"$randomEmail": func() string {
		return strings.ToLower(pick(firstNames)) + "." + strings.ToLower(strings.ReplaceAll(pick(lastNames), "-", "")) + "@example." + pick(tlds)
	},
	"$randomExampleEmail": func() string { return strings.ToLower(pick(firstNames)) + "@example.com" },
	"$randomPassword":     func() string { return alnum(15) },
	"$randomPhoneNumber": func() string {
		return fmt.Sprintf("%03d-%03d-%04d", rand.IntN(900)+100, rand.IntN(1000), rand.IntN(10000))
	},
	"$randomCity":          func() string { return pick(cities) },
	"$randomCountry":       func() string { return pick(countries) },
	"$randomCountryCode":   func() string { return pick(ccodes) },
	"$randomStreetName":    func() string { return pick(streets) },
	"$randomStreetAddress": func() string { return strconv.Itoa(rand.IntN(9999)+1) + " " + pick(streets) },
	"$randomIP": func() string {
		return fmt.Sprintf("%d.%d.%d.%d", rand.IntN(256), rand.IntN(256), rand.IntN(256), rand.IntN(256))
	},
	"$randomIPV6": func() string {
		p := make([]string, 8)
		for i := range p {
			p[i] = fmt.Sprintf("%x", rand.IntN(65536))
		}
		return strings.Join(p, ":")
	},
	"$randomMACAddress": func() string {
		p := make([]string, 6)
		for i := range p {
			p[i] = fmt.Sprintf("%02x", rand.IntN(256))
		}
		return strings.Join(p, ":")
	},
	"$randomDomainName": func() string { return pick(words) + "-" + pick(nouns) + "." + pick(tlds) },
	"$randomDomainWord": func() string { return pick(words) },
	"$randomUrl":        func() string { return "https://" + pick(words) + "." + pick(tlds) },
	"$randomColor":      func() string { return pick(colors) },
	"$randomHexColor":   func() string { return fmt.Sprintf("#%06x", rand.IntN(0xffffff+1)) },
	"$randomWord":       func() string { return pick(words) },
	"$randomWords": func() string {
		return pick(words) + " " + pick(words) + " " + pick(words)
	},
	"$randomNoun":      func() string { return pick(nouns) },
	"$randomVerb":      func() string { return pick(verbs) },
	"$randomAdjective": func() string { return pick(adjectives) },
	"$randomLoremWord": func() string { return pick(lorem) },
	"$randomLoremWords": func() string {
		return pick(lorem) + " " + pick(lorem) + " " + pick(lorem)
	},
	"$randomLoremSentence": func() string {
		n := rand.IntN(6) + 4
		w := make([]string, n)
		for i := range w {
			w[i] = pick(lorem)
		}
		s := strings.Join(w, " ")
		return strings.ToUpper(s[:1]) + s[1:] + "."
	},
	"$randomLoremParagraph": func() string {
		return strings.Join(lorem, " ") + "."
	},
	"$randomPrice":        func() string { return fmt.Sprintf("%.2f", rand.Float64()*1000) },
	"$randomCurrencyCode": func() string { return pick(currencies) },
	"$randomDateFuture":   func() string { return randomDate(365) },
	"$randomDatePast":     func() string { return randomDate(-365) },
	"$randomDateRecent":   func() string { return randomDate(-2) },
	"$randomJobTitle":     func() string { return pick(adjectives) + " " + pick(jobs) },
	"$randomCompanyName":  func() string { return pick(companies) },
	"$randomLocale":       func() string { return pick(locales) },
	"$randomSemver": func() string {
		return fmt.Sprintf("%d.%d.%d", rand.IntN(10), rand.IntN(20), rand.IntN(50))
	},
	"$randomUserAgent": func() string {
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	},
}

// Dynamic evaluates a Postman dynamic variable such as $guid.
func Dynamic(name string) (string, bool) {
	if f, ok := dynamic[name]; ok {
		return f(), true
	}
	return "", false
}

// DynamicNames lists the supported dynamic variables (for autocompletion).
func DynamicNames() []string {
	out := make([]string, 0, len(dynamic))
	for k := range dynamic {
		out = append(out, k)
	}
	return out
}
