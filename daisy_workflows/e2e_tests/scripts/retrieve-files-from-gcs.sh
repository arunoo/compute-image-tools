#!/bin/bash

retrieve_me='gs://gce-daisy-test-sandbox/copy-gcs-object-test.txt'
gsutil cp $retrieve_me . && gsutil rm $retrieve_me && echo 'SUCCESS wVnWw3a41CVe3mBVvTMn' || echo 'FAILURE wVnWw3a41CVe3mBVvTMn'

