package loge

import (
	"fmt"
	"crypto/rand"
	"time"
	"sync"
)

type LogeTypeMap map[string]*LogeType
type LogeObjectMap map[string]map[string]*LogeObject

type LogeDB struct {
	types LogeTypeMap
	objects LogeObjectMap
	mutex sync.Mutex
}

func NewLogeDB() *LogeDB {
	return &LogeDB {
		types: make(LogeTypeMap),
		objects: make(LogeObjectMap),
	}
}

func (db *LogeDB) CreateType(name string, exemplar interface{}) *LogeType {
	_, ok := db.types[name]

	if ok {
		panic(fmt.Sprintf("Type exists: '%s'", name))
	}

	var t = &LogeType {
		Name: name,
		Version: 1,
		Exemplar: exemplar,
	}
	db.types[name] = t
	return t
}


func (db *LogeDB) CreateTransaction() *Transaction {
	return NewTransaction(db)
}


type Transactor func(*Transaction)

func (db *LogeDB) Transact(actor Transactor, timeout time.Duration) bool {
	var start = time.Now()
	for {
		var t = db.CreateTransaction()
		actor(t)
		if t.Commit() {
			return true
		}
		if timeout > 0 && time.Since(start) > timeout {
			break
		}
	}
	return false
}


func (db *LogeDB) CreateObj(typeName string, key string) *LogeObject {
	lt, ok := db.types[typeName]

	if !ok {
		panic(fmt.Sprintf("Unknown type '%s'", typeName))
	}

	if key == "" {
		key = RandomLogeKey()
	}

	return InitializeObject(key, db, lt)
}



func (db *LogeDB) GetObj(typeName string, key string) *LogeObject {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	// TODO: Loading!
	objMap, ok := db.objects[typeName]
	if !ok {
		objMap = make(map[string]*LogeObject)
		db.objects[typeName] = objMap
	}

	obj, ok := objMap[key]
	if !ok {
		return nil
	}

	return obj
}


func (db *LogeDB) EnsureObj(obj *LogeObject) *LogeObject {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	var typeName = obj.Type.Name
	var key = obj.Key

	objMap, ok := db.objects[typeName]

	if !ok {
		objMap = make(map[string]*LogeObject)
		db.objects[typeName] = objMap
	}

	existing, ok := objMap[key]

	if ok {
		return existing
	}

	objMap[key] = obj
	return obj
}


// For testing only
func (db *LogeDB) Keys() []string {
	var keys []string
	for k := range db.objects {
		keys = append(keys, k)
	}
	return keys
}


func RandomLogeKey() string {
	var buf = make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		panic("Couldn't generate key")
	}
	return fmt.Sprintf("%x", buf)
}
