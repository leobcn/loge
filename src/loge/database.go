package loge

import (
	"fmt"
	"time"
)

type LogeDB struct {
	types typeMap
	store LogeStore
	cache objCache
	lock spinLock
}

func NewLogeDB(store LogeStore) *LogeDB {
	return &LogeDB {
		types: make(typeMap),
		store: store,
		cache: make(objCache),
	}
}


type typeMap map[string]*logeType

type objCache map[string]*logeObject

type objRef struct {
	TypeName string
	Key LogeKey
	LinkName string
	CacheKey string
}

type Transactor func(*Transaction)


func makeObjRef(typeName string, key LogeKey) objRef {
	var cacheKey = typeName + "^" + string(key)
	return objRef { typeName, key, "", cacheKey }
}

func makeLinkRef(typeName string, linkName string, key LogeKey) objRef {
	var cacheKey = "^" + typeName + "^" + linkName + "^" + string(key)
	return objRef { typeName, key, linkName, cacheKey }
}

func (objRef objRef) String() string {
	return objRef.CacheKey
}

func (objRef objRef) IsLink() bool {
	return objRef.LinkName != ""
}


func (db *LogeDB) Close() {
	db.store.close()
}

func (db *LogeDB) CreateType(name string, version uint16, exemplar interface{}, linkSpec LinkSpec) *logeType {
	_, ok := db.types[name]

	if ok {
		panic(fmt.Sprintf("Type exists: '%s'", name))
	}

	var infos = make(map[string]*linkInfo)
	for k, v := range linkSpec {
		infos[k] = &linkInfo{
			Name: k,
			Target: v,
			Tag: 0,
		}
	}

	var t = &logeType {
		Name: name,
		Version: version,
		Exemplar: exemplar,
		Links: infos,
	}

	db.types[name] = t
	db.store.registerType(t)
	
	return t
}


func (db *LogeDB) CreateTransaction() *Transaction {
	return NewTransaction(db)
}

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

func (db *LogeDB) Find(typeName string, linkName string, target LogeKey) ResultSet {	
	typ, ok := db.types[typeName]
	if !ok {
		panic(fmt.Sprintf("Type does not exist: %s", typeName))
	}
	return db.store.find(typ, linkName, target)
}


func (db *LogeDB) FlushCache() int {
	var count = 0
	db.lock.SpinLock()
	defer db.lock.Unlock()
	for key, obj := range db.cache {
		if obj.RefCount == 0 {
			delete(db.cache, key)
			count++
		}
	}
	return count
}


// -----------------------------------------------
// Internals
// -----------------------------------------------

func (db *LogeDB) ensureObj(ref objRef, load bool) *logeObject {
	var typeName = ref.TypeName
	var key = ref.Key

	var objKey = ref.String()
	var typ = db.types[typeName]

	db.lock.SpinLock()
	var obj, ok = db.cache[objKey]

	if ok && (obj.Loaded || !load) {
		db.lock.Unlock()
		return obj
	}

	if !ok {
		obj = initializeObject(db, typ, key)
	}

	obj.Lock.SpinLock()
	defer obj.Lock.Unlock()

	db.cache[objKey] = obj	

	db.lock.Unlock()

	var version *objectVersion
	if ref.IsLink() { 
		var links []string
		if load {
			links = db.store.getLinks(typ, ref.LinkName, key)
			obj.Loaded = true
		}

		var linkSet = newLinkSet()
		linkSet.Original = links
		version = &objectVersion {
			LogeObj: obj,
			Version: 0,
			Object: linkSet,
		}
		obj.LinkName = ref.LinkName

	} else {
		var object interface{}
		
		if load {
			object = db.store.get(typ, key)
			obj.Loaded = true
		}

		if object == nil {
			object = typ.NilValue()
		}

		version = &objectVersion{
			Version: 0,
			Object: object,
		}

		version.LogeObj = obj
	}

	obj.Current = version
	return obj
}


