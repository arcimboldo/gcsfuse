// Copyright 2015 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gcsproxy

import (
	"crypto/rand"
	"fmt"
	"io"

	"github.com/jacobsa/gcloud/gcs"
	"golang.org/x/net/context"
)

// Create an objectCreator that accepts a source object and the contents that
// should be "appended" to it, storing temporary objects using the supplied
// prefix.
//
// Note that the Create method will attempt to remove any temporary junk left
// behind, but it may fail to do so. Users should arrange for garbage collection.
func newAppendObjectCreator(
	prefix string,
	bucket gcs.Bucket) (oc objectCreator) {
	oc = &appendObjectCreator{
		prefix: prefix,
		bucket: bucket,
	}

	return
}

////////////////////////////////////////////////////////////////////////
// Implementation
////////////////////////////////////////////////////////////////////////

type appendObjectCreator struct {
	prefix string
	bucket gcs.Bucket
}

func (oc *appendObjectCreator) chooseName() (name string, err error) {
	// Generate a good 64-bit random number.
	var buf [8]byte
	_, err = io.ReadFull(rand.Reader, buf[:])
	if err != nil {
		err = fmt.Errorf("ReadFull: %v", err)
		return
	}

	x := uint64(buf[0])<<0 |
		uint64(buf[1])<<8 |
		uint64(buf[2])<<16 |
		uint64(buf[3])<<24 |
		uint64(buf[4])<<32 |
		uint64(buf[5])<<40 |
		uint64(buf[6])<<48 |
		uint64(buf[7])<<56

	// Turn it into a name.
	name = fmt.Sprintf("%s%016x", oc.prefix, x)

	return
}

func (oc *appendObjectCreator) Create(
	ctx context.Context,
	srcObject *gcs.Object,
	r io.Reader) (o *gcs.Object, err error) {
	// Choose a name for a temporary object.
	tmpName, err := oc.chooseName()
	if err != nil {
		err = fmt.Errorf("chooseName: %v", err)
		return
	}

	// Create a temporary object containing the additional contents.
	var zero int64
	tmp, err := oc.bucket.CreateObject(
		ctx,
		&gcs.CreateObjectRequest{
			Name: tmpName,
			GenerationPrecondition: &zero,
			Contents:               r,
		})

	if err != nil {
		// Don't mangle precondition errors.
		if _, ok := err.(*gcs.PreconditionError); ok {
			return
		}

		err = fmt.Errorf("CreateObject: %v", err)
		return
	}

	// Attempt to delete the temporary object when we're done.
	defer func() {
		deleteErr := oc.bucket.DeleteObject(ctx, tmp.Name)
		if err == nil && deleteErr != nil {
			err = fmt.Errorf("DeleteObject: %v", deleteErr)
		}
	}()

	// Compose the old contents plus the new over the old.
	o, err = oc.bucket.ComposeObjects(
		ctx,
		&gcs.ComposeObjectsRequest{
			DstName:                   srcObject.Name,
			DstGenerationPrecondition: &srcObject.Generation,
			Sources: []gcs.ComposeSource{
				gcs.ComposeSource{
					Name:       srcObject.Name,
					Generation: srcObject.Generation,
				},

				gcs.ComposeSource{
					Name:       tmp.Name,
					Generation: tmp.Generation,
				},
			},
		})

	if err != nil {
		// Don't mangle precondition errors.
		if _, ok := err.(*gcs.PreconditionError); ok {
			return
		}

		err = fmt.Errorf("ComposeObjects: %v", err)
		return
	}

	return
}
