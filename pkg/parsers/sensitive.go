package parsers

import (
	"reflect"
	"regexp"
)

var secretPatterns = map[string]string{
	"Password in URL":               `[a-zA-Z]{3,10}:\\/\\/[^\\/\\s:@]{3,20}:[^\\/\\s:@]{3,20}@.{1,100}["'\\s]`,
	"Generic API Key":               `[a|A][p|P][i|I][_]?k[e|E]y[Y].*[\'|"][0-9a-zA-Z]{32,45}[\'|"]`,
	"Generic Secret":                `[s|S][e|E][c|C][r|R][e|E][t|T].*[\'|"][0-9a-zA-Z]{32,45}[\'|"]`,
	"Google API Key":                `AIza[0-9A-Za-z\\-_]{35}`,
	"Firebase URL":                  `.*firebaseio\\.com`,
	"RSA private key":               `-----BEGIN RSA PRIVATE KEY-----`,
	"SSH (DSA) private key":         `-----BEGIN DSA PRIVATE KEY-----`,
	"SSH (EC) private key":          `-----BEGIN EC PRIVATE KEY-----`,
	"PGP private key block":         `-----BEGIN PGP PRIVATE KEY BLOCK-----`,
	"Amazon AWS Access Key ID":      `AKIA[0-9A-Z]{16}`,
	"Amazon MWS Auth Token":         `amzn\\.mws\\.[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`,
	"AWS API Key":                   `AKIA[0-9A-Z]{16}`,
	"GitHub":                        `[g|G][i|I][t|T][h|H][u|U][b|B].*[\'|"][0-9a-zA-Z]{35,40}[\'|"]`,
	"Google Cloud Platform API Key": `AIza[0-9A-Za-z\\-_]{35}`,
	"Google Cloud Platform OAuth":   `[0-9]+-[0-9A-Za-z_]{32}\\.apps\\.googleusercontent\\.com`,
	"Google (GCP) Service-account":  `"type": "service_account"`,
	"Google OAuth Access Token":     `ya29\\.[0-9A-Za-z\\-_]+`,
	"Connection String":             `[a-zA-Z]+:\\/\\/[^/\\s]+:[^/\\s]+@[^/\\s]+\\/[^/\\s]+`,
}

var compiledPatterns []*regexp.Regexp

func init() {
	for _, pattern := range secretPatterns {
		compiledPatterns = append(compiledPatterns, regexp.MustCompile(pattern))
	}
}

func maskString(input string) string {
	maskedString := input
	for _, pattern := range compiledPatterns {
		maskedString = pattern.ReplaceAllString(maskedString, "[REDACTED]")
	}
	return maskedString
}

func ParseSensitiveData(data interface{}) interface{} {
	switch v := reflect.ValueOf(data); v.Kind() {
	case reflect.String:
		return maskString(v.String())
	case reflect.Slice:
		sliceCopy := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			sliceCopy.Index(i).Set(reflect.ValueOf(ParseSensitiveData(v.Index(i).Interface())))
		}
		return sliceCopy.Interface()
	case reflect.Map:
		mapCopy := reflect.MakeMap(v.Type())
		for _, key := range v.MapKeys() {
			mapCopy.SetMapIndex(key, reflect.ValueOf(ParseSensitiveData(v.MapIndex(key).Interface())))
		}
		return mapCopy.Interface()
	default:
		return data
	}
}
