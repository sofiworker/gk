package gkv

type KV interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}
