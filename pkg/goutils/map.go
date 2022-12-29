package goutils

func MergeMaps(ms ...map[string]interface{}) map[string]interface{} {
	res := map[string]interface{}{}
	for _, m := range ms {
		for k, v := range m {
			res[k] = v
		}
	}
	return res
}
