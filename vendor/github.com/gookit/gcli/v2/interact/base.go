// Package interact collect some interactive methods for CLI
package interact

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gookit/color"
)

const (
	// OK success exit code
	OK = 0
	// ERR error exit code
	ERR = 2
)

// RunFace for interact methods
type RunFace interface {
	Run() *Value
}

// Value data store
type Value struct {
	// key interface{} // string, []string
	val interface{}
}

// Set val
func (v Value) Set(val interface{}) {
	v.val = val
}

// Val get
func (v Value) Val() interface{} {
	return v.val
}

// Int value
func (v Value) Int() (val int) {
	if v.val == nil {
		return
	}
	switch tpVal := v.val.(type) {
	case int:
		return tpVal
	case string:
		val, err := strconv.Atoi(tpVal)
		if err == nil {
			return val
		}
	}
	return
}

// String value
func (v Value) String() string {
	if v.val == nil {
		return ""
	}

	return fmt.Sprintf("%v", v.val)
}

// Strings value
func (v Value) Strings() (ss []string) {
	if v.val == nil {
		return
	}

	return v.val.([]string)
}

// IsEmpty value
func (v Value) IsEmpty() bool {
	return v.val == nil
}

/*************************************************************
 * helper methods
 *************************************************************/

func exitWithErr(format string, v ...interface{}) {
	color.Error.Tips(format, v...)
	os.Exit(ERR)
}

func exitWithMsg(exitCode int, messages ...interface{}) {
	fmt.Println(messages...)
	os.Exit(exitCode)
}

func intsToMap(is []int) map[string]string {
	ms := make(map[string]string, len(is))
	for i, val := range is {
		k := fmt.Sprint(i)
		ms[k] = fmt.Sprint(val)
	}

	return ms
}

func stringToArr(str, sep string) (arr []string) {
	str = strings.TrimSpace(str)
	ss := strings.Split(str, sep)
	for _, val := range ss {
		if val = strings.TrimSpace(val); val != "" {
			arr = append(arr, val)
		}
	}

	return arr
}

func stringsToMap(ss []string) map[string]string {
	ms := make(map[string]string, len(ss))
	for i, val := range ss {
		k := fmt.Sprint(i)
		ms[k] = val
	}

	return ms
}
