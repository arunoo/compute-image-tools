//  Copyright 2017 Google Inc. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package compute

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/kylelemons/godebug/pretty"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

var (
	testProject  = "test-project"
	testZone     = "test-zone"
	testDisk     = "test-disk"
	testImage    = "test-image"
	testInstance = "test-instance"
	testNetwork  = "test-network"
)

func TestShouldRetryWithWait(t *testing.T) {
	tests := []struct {
		desc string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"non googleapi.Error", errors.New("foo"), false},
		{"400 error", &googleapi.Error{Code: 400}, false},
		{"429 error", &googleapi.Error{Code: 429}, true},
		{"500 error", &googleapi.Error{Code: 500}, true},
	}

	for _, tt := range tests {
		if got := shouldRetryWithWait(nil, tt.err, 0); got != tt.want {
			t.Errorf("%s case: shouldRetryWithWait == %t, want %t", tt.desc, got, tt.want)
		}
	}
}

func TestCreates(t *testing.T) {
	var getURL, insertURL *string
	var getErr, insertErr, waitErr error
	var getResp interface{}
	svr, c, err := NewTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		url := r.URL.String()
		if r.Method == "POST" && url == *insertURL {
			if insertErr != nil {
				w.WriteHeader(400)
				fmt.Fprintln(w, insertErr)
				return
			}
			buf := new(bytes.Buffer)
			if _, err := buf.ReadFrom(r.Body); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(w, `{}`)
		} else if r.Method == "GET" && url == *getURL {
			if getErr != nil {
				w.WriteHeader(400)
				fmt.Fprintln(w, getErr)
				return
			}
			body, _ := json.Marshal(getResp)
			fmt.Fprintln(w, string(body))
		} else {
			w.WriteHeader(500)
			fmt.Fprintln(w, "URL and Method not recognized:", r.Method, url)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer svr.Close()
	c.operationsWaitFn = func(project, zone, name string) error { return waitErr }

	tests := []struct {
		desc                       string
		getErr, insertErr, waitErr error
		shouldErr                  bool
	}{
		{"normal case", nil, nil, nil, false},
		{"get err case", errors.New("get err"), nil, nil, true},
		{"insert err case", nil, errors.New("insert err"), nil, true},
		{"wait err case", nil, nil, errors.New("wait err"), true},
	}

	d := &compute.Disk{Name: testDisk}
	im := &compute.Image{Name: testImage}
	in := &compute.Instance{Name: testInstance}
	n := &compute.Network{Name: testNetwork}
	creates := []struct {
		name              string
		do                func() error
		getURL, insertURL string
		getResp, resource interface{}
	}{
		{
			"disks",
			func() error { return c.CreateDisk(testProject, testZone, d) },
			fmt.Sprintf("/%s/zones/%s/disks/%s?alt=json", testProject, testZone, testDisk),
			fmt.Sprintf("/%s/zones/%s/disks?alt=json", testProject, testZone),
			&compute.Disk{Name: testDisk, SelfLink: "foo"},
			d,
		},
		{
			"images",
			func() error { return c.CreateImage(testProject, im) },
			fmt.Sprintf("/%s/global/images/%s?alt=json", testProject, testImage),
			fmt.Sprintf("/%s/global/images?alt=json", testProject),
			&compute.Image{Name: testImage, SelfLink: "foo"},
			im,
		},
		{
			"instances",
			func() error { return c.CreateInstance(testProject, testZone, in) },
			fmt.Sprintf("/%s/zones/%s/instances/%s?alt=json", testProject, testZone, testInstance),
			fmt.Sprintf("/%s/zones/%s/instances?alt=json", testProject, testZone),
			&compute.Instance{Name: testImage, SelfLink: "foo"},
			in,
		},
		{
			"networks",
			func() error { return c.CreateNetwork(testProject, n) },
			fmt.Sprintf("/%s/global/networks/%s?alt=json", testProject, testNetwork),
			fmt.Sprintf("/%s/global/networks?alt=json", testProject),
			&compute.Network{Name: testNetwork, SelfLink: "foo"},
			n,
		},
	}

	for _, create := range creates {
		getURL = &create.getURL
		insertURL = &create.insertURL
		getResp = create.getResp
		for _, tt := range tests {
			getErr, insertErr, waitErr = tt.getErr, tt.insertErr, tt.waitErr
			create.do()

			// We have to fudge this part in order to check that the returned resource == getResp.
			f := reflect.ValueOf(create.resource).Elem().FieldByName("ServerResponse")
			f.Set(reflect.Zero(f.Type()))

			if err != nil && !tt.shouldErr {
				t.Errorf("%s: got unexpected error: %s", tt.desc, err)
			} else if diff := pretty.Compare(create.resource, getResp); err == nil && diff != "" {
				t.Errorf("%s: Resource does not match expectation: (-got +want)\n%s", tt.desc, diff)
			}
		}
	}
}

func TestDeletes(t *testing.T) {
	var deleteURL, opGetURL *string
	svr, c, err := NewTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" && r.URL.String() == *deleteURL {
			fmt.Fprint(w, `{}`)
		} else if r.Method == "GET" && r.URL.String() == *opGetURL {
			fmt.Fprint(w, `{"Status":"DONE"}`)
		} else {
			w.WriteHeader(500)
			fmt.Fprintln(w, "URL and Method not recognized:", r.Method, r.URL)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer svr.Close()

	deletes := []struct {
		name                string
		do                  func() error
		deleteURL, opGetURL string
	}{
		{
			"disks",
			func() error { return c.DeleteDisk(testProject, testZone, testDisk) },
			fmt.Sprintf("/%s/zones/%s/disks/%s?alt=json", testProject, testZone, testDisk),
			fmt.Sprintf("/%s/zones/%s/operations/?alt=json", testProject, testZone),
		},
		{
			"images",
			func() error { return c.DeleteImage(testProject, testImage) },
			fmt.Sprintf("/%s/global/images/%s?alt=json", testProject, testImage),
			fmt.Sprintf("/%s/global/operations/?alt=json", testProject),
		},
		{
			"instances",
			func() error { return c.DeleteInstance(testProject, testZone, testInstance) },
			fmt.Sprintf("/%s/zones/%s/instances/%s?alt=json", testProject, testZone, testInstance),
			fmt.Sprintf("/%s/zones/%s/operations/?alt=json", testProject, testZone),
		},
		{
			"networks",
			func() error { return c.DeleteNetwork(testProject, testNetwork) },
			fmt.Sprintf("/%s/global/networks/%s?alt=json", testProject, testNetwork),
			fmt.Sprintf("/%s/global/operations/?alt=json", testProject),
		},
	}

	for _, d := range deletes {
		deleteURL = &d.deleteURL
		opGetURL = &d.opGetURL
		if err := d.do(); err != nil {
			t.Errorf("%s: error running Delete: %v", d.name, err)
		}
	}
}

func TestDeprecateImage(t *testing.T) {
	svr, c, err := NewTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.String() == fmt.Sprintf("/%s/global/images/%s/deprecate?alt=json", testProject, testImage) {
			fmt.Fprint(w, `{}`)
		} else if r.Method == "GET" && r.URL.String() == fmt.Sprintf("/%s/global/operations/?alt=json", testProject) {
			fmt.Fprint(w, `{"Status":"DONE"}`)
		} else {
			w.WriteHeader(500)
			fmt.Fprintln(w, "URL and Method not recognized:", r.Method, r.URL)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer svr.Close()

	if err := c.DeprecateImage(testProject, testImage, &compute.DeprecationStatus{}); err != nil {
		t.Fatalf("error running DeprecateImage: %v", err)
	}
}
