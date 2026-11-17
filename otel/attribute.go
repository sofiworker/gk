package gotel

type KeyValue struct {
	Key   string
	Value interface{}
}

func KV(key string, value interface{}) KeyValue {
	return KeyValue{Key: key, Value: value}
}
