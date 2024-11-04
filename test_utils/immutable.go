package testing_init

import (
	"fmt"
	"strings"
)

func GetBlueprintIdFromPrefixAndStateKey(blueprintPrefix string, stateKey string) string {
	stateKeySplit := strings.Split(stateKey, "-")
	return fmt.Sprintf("%s-%s", blueprintPrefix, stateKeySplit[len(stateKeySplit)-1])
}
