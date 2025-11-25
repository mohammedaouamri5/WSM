package wsm

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
)

func HashToShortString(data []byte) (string) {
	hash := sha256.Sum256(data)
	hexStr := hex.EncodeToString(hash[:])
	return hexStr[:10]
}



func getCallerInfo() (funcName, file string, line int) {
	// skip = 1: skip getCallerInfo itself, get the immediate caller
	pc, file, line, ok := runtime.Caller(1)
	if !ok {
		return "???", "???", 0
	}
	funcName = runtime.FuncForPC(pc).Name()
	return funcName, file, line
}


