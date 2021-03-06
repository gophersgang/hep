package sio

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"unsafe"
)

// recordHeader describes the on-disk record (header part)
type recordHeader struct {
	Len uint32
	Typ uint32
}

// recordData describes the on-disk record (payload part)
type recordData struct {
	Options uint32
	DataLen uint32 // length of compressed record data
	UCmpLen uint32 // length of uncompressed record data
	NameLen uint32 // length of record name
}

// Record manages blocks of data
type Record struct {
	name    string           // record name
	unpack  bool             // whether to unpack incoming records
	options uint32           // options (flag word)
	blocks  map[string]Block // connected blocks
}

// Name returns the name of this record
func (rec *Record) Name() string {
	return rec.name
}

// Unpack returns whether to unpack incoming records
func (rec *Record) Unpack() bool {
	return rec.unpack
}

// SetUnpack sets whether to unpack incoming records
func (rec *Record) SetUnpack(unpack bool) {
	rec.unpack = unpack
}

// Compress returns the compression flag
func (rec *Record) Compress() bool {
	return rec.options&g_opt_compress != 0
}

// SetCompress sets or resets the compression flag
func (rec *Record) SetCompress(compress bool) {
	rec.options &= g_opt_not_compress
	if compress {
		rec.options |= g_opt_compress
	}
}

// Options returns the options of this record.
func (rec *Record) Options() uint32 {
	return rec.options
}

// Connect connects a Block to this Record (for reading or writing)
func (rec *Record) Connect(name string, ptr interface{}) error {
	var err error
	_, dup := rec.blocks[name]
	if dup {
		//return fmt.Errorf("sio.Record: Block name [%s] already connected", name)
		//return ErrBlockConnected
	}
	var block Block
	switch ptr := ptr.(type) {
	case Block:
		block = ptr
	case BinaryCodec:
		rt := reflect.TypeOf(ptr)
		block = &mBlockImpl{
			blk:     ptr,
			version: 0,
			name:    rt.Name(),
		}

	default:
		rt := reflect.TypeOf(ptr)
		if rt.Kind() != reflect.Ptr {
			return fmt.Errorf("sio: Connect needs a pointer to a block of data")
		}
		block = &blockImpl{
			rt:      rt,
			rv:      reflect.ValueOf(ptr),
			version: 0,
			name:    rt.Name(),
		}
	}
	rec.blocks[name] = block
	return err
}

// read reads a record
func (rec *Record) read(buf *bytes.Buffer) error {
	var err error
	//fmt.Printf("::: reading record [%s]... [%d]\n", rec.name, buf.Len())
	// loop until data has been depleted
	for buf.Len() > 0 {
		// read block header
		var hdr blockHeader
		err = bread(buf, &hdr)
		if err != nil {
			return err
		}
		if hdr.Typ != g_mark_block {
			// fmt.Printf("*** err record[%s]: noblockmarker\n", rec.name)
			return ErrRecordNoBlockMarker
		}

		var data blockData
		err = bread(buf, &data)
		if err != nil {
			return err
		}

		var cbuf bytes.Buffer
		nlen := align4(data.NameLen)
		n, err := io.CopyN(&cbuf, buf, int64(nlen))
		if err != nil {
			// fmt.Printf(">>> err:%v\n", err)
			return err
		}
		if n != int64(nlen) {
			return fmt.Errorf("sio: read too few bytes (got=%d. expected=%d)", n, nlen)
		}
		name := string(cbuf.Bytes()[:data.NameLen])
		blk, ok := rec.blocks[name]
		if !ok {
			// fmt.Printf("*** no block [%s]. draining buffer!\n", name)
			// drain the whole buffer
			buf.Next(buf.Len())
			continue
		}
		//fmt.Printf("### %q\n", string(buf.Bytes()))
		err = blk.UnmarshalBinary(buf)
		if err != nil {
			// fmt.Printf("*** error unmarshaling record=%q block=%q: %v\n", rec.name, name, err)
			return err
		}
		//fmt.Printf(">>> read record=%q block=%q (buf=%d)\n", rec.name, name, buf.Len())

		// check whether there is still something to be read.
		// if there is, check whether there is a block-marker
		if buf.Len() > 0 {
			rest := buf.Bytes()
			idx := bytes.Index(rest, g_mark_block_b)
			if idx > 0 {
				buf.Next(idx - 8 /*sizeof blockHeader*/)
			} else {
				buf.Next(buf.Len())
			}
		}
	}
	//fmt.Printf("::: reading record [%s]... [done]\n", rec.name)
	return err
}

func (rec *Record) write(buf *bytes.Buffer) error {
	var err error
	for k, blk := range rec.blocks {

		bhdr := blockHeader{
			Typ: g_mark_block,
		}

		bdata := blockData{
			Version: blk.Version(),
			NameLen: uint32(len(k)),
		}

		var b bytes.Buffer
		err = blk.MarshalBinary(&b)
		if err != nil {
			return err
		}

		bhdr.Len = uint32(unsafe.Sizeof(bhdr)) +
			uint32(unsafe.Sizeof(bdata)) +
			align4(bdata.NameLen) + uint32(b.Len())

		// fmt.Printf("blockHeader: %v\n", bhdr)
		// fmt.Printf("blockData:   %v (%s)\n", bdata, k)

		err = bwrite(buf, &bhdr)
		if err != nil {
			return err
		}

		err = bwrite(buf, &bdata)
		if err != nil {
			return err
		}

		_, err = buf.Write([]byte(k))
		if err != nil {
			return err
		}
		padlen := align4(bdata.NameLen) - bdata.NameLen
		if padlen > 0 {
			_, err = buf.Write(make([]byte, int(padlen)))
			if err != nil {
				return err
			}
		}

		_, err := io.Copy(buf, &b)
		if err != nil {
			return err
		}
	}
	return err
}

// EOF
