package loge

import (
	"fmt"
	"time"
	"math/rand"
)

type TransactionState int

const (
	ACTIVE = iota
	COMMITTING
	FINISHED
	ABORTED
	ERROR
)


type Transaction struct {
	db *LogeDB
	context transactionContext
	versions map[string]*objectVersion
	state TransactionState
	snapshotID uint64
}

func NewTransaction(db *LogeDB, sID uint64) *Transaction {
	return &Transaction{
		db: db,
		context: db.store.newContext(),
		versions: make(map[string]*objectVersion),
		state: ACTIVE,
		snapshotID: sID,
	}
}


func (t *Transaction) String() string {
	return fmt.Sprintf("Transaction<%s>", t.state.String())
}

func (t *Transaction) GetState() TransactionState {
	return t.state
}

func (t *Transaction) Exists(typeName string, key LogeKey) bool {
	var version = t.getObj(makeObjRef(typeName, key), false, true)
	return version.hasValue()
}


func (t *Transaction) Read(typeName string, key LogeKey) interface{} {
	return t.getObj(makeObjRef(typeName, key), false, true).Object
}


func (t *Transaction) Write(typeName string, key LogeKey) interface{} {
	return t.getObj(makeObjRef(typeName, key), true, true).Object
}


func (t *Transaction) Set(typeName string, key LogeKey, obj interface{}) {
	var version = t.getObj(makeObjRef(typeName, key), true, false)
	version.Object = obj
}


func (t *Transaction) Delete(typeName string, key LogeKey) {
	var version = t.getObj(makeObjRef(typeName, key), true, true)
	version.Object = version.LogeObj.Type.NilValue()
}


func (t *Transaction) ReadLinks(typeName string, linkName string, key LogeKey) []string {
	return t.getLink(makeLinkRef(typeName, linkName, key), false, true).ReadKeys()
}

func (t *Transaction) HasLink(typeName string, linkName string, key LogeKey, target LogeKey) bool {
	return t.getLink(makeLinkRef(typeName, linkName, key), false, true).Has(string(target))
}

func (t *Transaction) AddLink(typeName string, linkName string, key LogeKey, target LogeKey) {
	t.getLink(makeLinkRef(typeName, linkName, key), true, true).Add(string(target))
}

func (t *Transaction) RemoveLink(typeName string, linkName string, key LogeKey, target LogeKey) {
	t.getLink(makeLinkRef(typeName, linkName, key), true, true).Remove(string(target))
}

func (t *Transaction) SetLinks(typeName string, linkName string, key LogeKey, targets []LogeKey) {
	// XXX BGH: Yargh
	var stringTargets = make([]string, 0, len(targets))
	for _, key := range targets {
		stringTargets = append(stringTargets, string(key))
	}
	t.getLink(makeLinkRef(typeName, linkName, key), true, true).Set(stringTargets)
}

func (t *Transaction) Find(typeName string, linkName string, target LogeKey) ResultSet {
	return t.context.find(t.db.types[typeName], linkName, target)
}

func (t *Transaction) FindFrom(typeName string, linkName string, target LogeKey, from LogeKey, limit int) ResultSet {	
	return t.context.findFrom(t.db.types[typeName], linkName, target, from, limit)
}

// -----------------------------------------------
// Internals
// -----------------------------------------------

func (t *Transaction) getLink(ref objRef, forWrite bool, load bool) *linkSet {
	var version = t.getObj(ref, forWrite, load)
	return version.Object.(*linkSet)
}

func (t *Transaction) getObj(ref objRef, forWrite bool, load bool) *objectVersion {

	if t.state != ACTIVE {
		panic(fmt.Sprintf("GetObj from inactive transaction %s\n", t))
	}

	var objKey = ref.String()

	version, ok := t.versions[objKey]

	if ok {
		if forWrite {
			if !version.Dirty {
				version = version.LogeObj.newVersion(t.snapshotID)
				t.versions[objKey] = version
			}
		}
		return version
	}

	var logeObj = t.db.ensureObj(ref, load)

	logeObj.Lock.SpinLock()
	defer logeObj.Lock.Unlock()

	logeObj.RefCount++

	if forWrite {
		version = logeObj.newVersion(t.snapshotID)
	} else {
		version = logeObj.getTransactionVersion(t.snapshotID)
	}

	t.versions[objKey] = version

	return version
}


const t_BACKOFF_EXPONENT = 1.05

func (t *Transaction) Commit() bool {
	
	if (t.state != ACTIVE) {
		panic(fmt.Sprintf("Commit on transaction %s\n", t))
	}

	t.state = COMMITTING
	
	var delayFact = 10.0
	for {
		if t.tryCommit() {
			break
		}
		var delay = time.Duration(delayFact - float64(rand.Intn(10)))
		time.Sleep(delay * time.Millisecond)
		delayFact *= t_BACKOFF_EXPONENT
	}

	return t.state == FINISHED
}

func (t *Transaction) tryCommit() bool {
	for _, version := range t.versions {
		var obj = version.LogeObj

		if !obj.Lock.TryLock() {
			return false
		}
		defer obj.Lock.Unlock()

		if obj.Current.snapshotID > t.snapshotID {
			t.state = ABORTED
			return true
		}
	}

	var context = t.context
	var sID = t.db.newSnapshotID()

	for _, version := range t.versions {
		if version.Dirty {
			version.LogeObj.applyVersion(version, context, sID)
		}
		version.LogeObj.RefCount--
	}

	var err = context.commit()
	if err != nil {
		t.state = ERROR
		fmt.Printf("Commit error: %v\n", err)
	}

	t.state = FINISHED
	return true
}


func (ts TransactionState) String() string {
	switch ts {
	case ACTIVE: 
		return "ACTIVE"
	case COMMITTING: 
		return "COMMITTING"
	case FINISHED: 
		return "FINISHED"
	case ABORTED: 
		return "ABORTED"
	case ERROR: 
		return "ERROR"
	}
	return "UNKNOWN STATE"
}