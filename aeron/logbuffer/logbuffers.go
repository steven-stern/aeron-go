/*
Copyright 2016 Stanislav Liberman

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package logbuffer

import (
	"github.com/lirm/aeron-go/aeron/atomic"
	"github.com/lirm/aeron-go/aeron/util/memmap"
	"github.com/op/go-logging"
	"unsafe"
)

var logger = logging.MustGetLogger("logbuffers")

// LogBuffers is the struct providing access to the file or files representing the terms containing the ring buffer
type LogBuffers struct {
	mmapFiles []*memmap.File
	buffers   [PartitionCount + 1]atomic.Buffer
	meta      LogBufferMetaData
}

// Wrap is the factory method wrapping the LogBuffers structure around memory mapped file
func Wrap(fileName string) *LogBuffers {
	buffers := new(LogBuffers)

	logLength := memmap.GetFileSize(fileName)
	termLength := computeTermLength(logLength)

	checkTermLength(termLength)

	if logLength < maxSingleMappingSize {
		mmap, err := memmap.MapExisting(fileName, 0, 0)
		if err != nil {
			panic(err)
		}

		buffers.mmapFiles = [](*memmap.File){mmap}
		basePtr := uintptr(buffers.mmapFiles[0].GetMemoryPtr())
		for i := 0; i < PartitionCount; i++ {
			ptr := unsafe.Pointer(basePtr + uintptr(int64(i)*termLength))
			buffers.buffers[i].Wrap(ptr, int32(termLength))
		}

		ptr := unsafe.Pointer(basePtr + uintptr(logLength-int64(logMetaDataLength)))
		buffers.buffers[LogMetaDataSectionIndex].Wrap(ptr, logMetaDataLength)
	} else {
		buffers.mmapFiles = make([](*memmap.File), PartitionCount+1)
		metaDataSectionOffset := termLength * int64(PartitionCount)
		metaDataSectionLength := int(logLength - metaDataSectionOffset)

		mmap, err := memmap.MapExisting(fileName, metaDataSectionOffset, metaDataSectionLength)
		if err != nil {
			panic("Failed to map the log buffer")
		}
		buffers.mmapFiles = append(buffers.mmapFiles, mmap)

		for i := 0; i < PartitionCount; i++ {
			// one map for each term
			mmap, err := memmap.MapExisting(fileName, int64(i)*termLength, int(termLength))
			if err != nil {
				panic("Failed to map the log buffer")
			}
			buffers.mmapFiles = append(buffers.mmapFiles, mmap)

			basePtr := buffers.mmapFiles[i+1].GetMemoryPtr()

			buffers.buffers[i].Wrap(basePtr, int32(termLength))
		}
		metaDataBasePtr := buffers.mmapFiles[0].GetMemoryPtr()
		buffers.buffers[LogMetaDataSectionIndex].Wrap(metaDataBasePtr, logMetaDataLength)
	}

	buffers.meta.Wrap(&buffers.buffers[PartitionCount], 0)

	return buffers
}

// Meta return log buffer meta data flyweight
func (logBuffers *LogBuffers) Meta() *LogBufferMetaData {
	return &logBuffers.meta
}

// Buffer returns a buffer backing a specific term based on index. PartitionLength+1 is the size of the buffer array,
// and the last buffer is the metadata buffer, which can be accessed through a convenience wrapped via Meta() method.
func (logBuffers *LogBuffers) Buffer(index int) *atomic.Buffer {
	return &logBuffers.buffers[index]
}

// Close will try to unmap all backing memory maps
func (logBuffers *LogBuffers) Close() error {
	logger.Debug("Closing logBuffers")
	// TODO accumulate errors
	var err error
	for _, mmap := range logBuffers.mmapFiles {
		err = mmap.Close()
	}
	logBuffers.mmapFiles = nil
	return err
}
