package gohook

import (
	"errors"
	"fmt"
	"reflect"
	"unsafe"
)

func dummy(v int) string {
	return fmt.Sprintf("some text:%d", v)
}

type CodeInfo struct {
	copy           bool
	Origin         []byte
	Fix            []CodeFix
	TrampolineOrig []byte
}

func makeSliceFromPointer(p uintptr, length int) []byte {
	return *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: p,
		Len:  length,
		Cap:  length,
	}))
}

func GetFuncInsSize(f interface{}) uint32 {
	sz := uint32(0)
	ptr := reflect.ValueOf(f).Pointer()
	if elfInfo != nil {
		sz, _ = elfInfo.GetFuncSize(ptr)
	}

	if sz == 0 {
		sz, _ = GetFuncSizeByGuess(GetArchMode(), ptr, true)
	}

	return sz
}

func CopyFunction(from, to interface{}, info *CodeInfo) ([]byte, error) {
	s := reflect.ValueOf(from).Pointer()
	d := reflect.ValueOf(to).Pointer()
	return doCopyFunction(GetArchMode(), s, d, info)
}

func doCopyFunction(mode int, from, to uintptr, info *CodeInfo) ([]byte, error) {
	sz1 := uint32(0)
	sz2 := uint32(0)
	if elfInfo != nil {
		sz1, _ = elfInfo.GetFuncSize(from)
		sz2, _ = elfInfo.GetFuncSize(to)
		// fmt.Printf("%x:%x,%x:%x\n",from, sz1, to, sz2)
	}

	var err error
	if sz1 == 0 {
		sz1, err = GetFuncSizeByGuess(mode, from, true)
		if err != nil {
			return nil, err
		}
	}

	if sz2 == 0 {
		sz2, err = GetFuncSizeByGuess(mode, to, true)
		if err != nil {
			return nil, err
		}
	}

	if sz1 > sz2+1 { // add trailing int3 to the end
		return nil, errors.New(fmt.Sprintf("source addr:%x, target addr:%x, sizeof source func(%d) > sizeof of target func(%d)", from, to, sz1, sz2))
	}

	fix, err2 := copyFuncInstruction(mode, from, to, int(sz1))
	if err2 != nil {
		return nil, err2
	}

	origin := makeSliceFromPointer(to, int(sz2))
	sf := make([]byte, sz2)
	copy(sf, origin)

	curAddr := to
	for _, f := range fix {
		CopyInstruction(curAddr, f.Code)
		f.Addr = curAddr
		curAddr += uintptr(len(f.Code))
	}

	info.Fix = fix
	return sf, nil
}

func hookFunction(mode int, target, replace, trampoline uintptr) (*CodeInfo, error) {
	info := &CodeInfo{}
	jumpcode := genJumpCode(mode, replace, target)

	insLen := len(jumpcode)
	if trampoline != uintptr(0) {
		f := makeSliceFromPointer(target, len(jumpcode)*2)
		insLen = GetInsLenGreaterThan(mode, f, len(jumpcode))
	}

	// target slice
	ts := makeSliceFromPointer(target, insLen)
	info.Origin = make([]byte, len(ts))
	copy(info.Origin, ts)

	if trampoline != uintptr(0) {
		sz := uint32(0)
		if elfInfo != nil {
			sz, _ = elfInfo.GetFuncSize(target)
		}

		fix, err := FixTargetFuncCode(mode, target, sz, trampoline, insLen)
		if err != nil {
			info.copy = true
			origin, err2 := doCopyFunction(mode, target, trampoline, info)
			if err2 != nil {
				return nil, errors.New(fmt.Sprintf("both fix and copy failed, fix:%s, copy:%s", err.Error(), err2.Error()))
			}
			info.TrampolineOrig = origin
		} else {
			info.copy = false
			for _, v := range fix {
				origin := makeSliceFromPointer(v.Addr, len(v.Code))
				f := make([]byte, len(v.Code))
				copy(f, origin)

				// printInstructionFix(v, f)

				CopyInstruction(v.Addr, v.Code)
				v.Code = f
				info.Fix = append(info.Fix, v)
			}

			jumpcode2 := genJumpCode(mode, target+uintptr(insLen), trampoline+uintptr(insLen))
			f2 := makeSliceFromPointer(trampoline, insLen+len(jumpcode2)*2)
			insLen2 := GetInsLenGreaterThan(mode, f2, insLen+len(jumpcode2))
			info.TrampolineOrig = make([]byte, insLen2)
			ts2 := makeSliceFromPointer(trampoline, insLen2)
			copy(info.TrampolineOrig, ts2)
			CopyInstruction(trampoline, ts)
			CopyInstruction(trampoline+uintptr(insLen), jumpcode2)
		}
	}

	CopyInstruction(target, jumpcode)
	return info, nil
}

func printInstructionFix(v CodeFix, origin []byte) {
	fmt.Printf("addr:0x%x, code:", v.Addr)
	for _, c := range v.Code {
		fmt.Printf(" %x", c)
	}

	fmt.Printf(", origin:")
	for _, c := range origin {
		fmt.Printf(" %x", c)
	}
	fmt.Printf("\n")
}

func GetFuncAddr(f interface{}) uintptr {
	fv := reflect.ValueOf(f)
	return fv.Pointer()
}
