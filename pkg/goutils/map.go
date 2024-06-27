package goutils

import "encoding/json"

func MergeMaps[T interface{}](ms ...map[string]T) map[string]T {
	res := map[string]T{}
	for _, m := range ms {
		for k, v := range m {
			res[k] = v
		}
	}
	return res
}

func StructToMap(obj interface{}) (newMap map[string]interface{}, err error) {
	data, err := json.Marshal(obj)

	if err != nil {
		return
	}

	err = json.Unmarshal(data, &newMap)
	return
}

func DeepCopy(obj interface{}) interface{} {
	if obj == nil {
		return nil
	}

	var newObj interface{}
	data, _ := json.Marshal(obj)
	json.Unmarshal(data, &newObj)

	return newObj
}
