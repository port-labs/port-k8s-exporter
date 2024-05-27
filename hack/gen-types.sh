#!/bin/bash

# This script generates the CRDs from the types of the k8s exporter

echo 'Installing required tools'

go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

echo 'Generating types...'

controller-gen paths="./pkg/api/..." crd 

if [ $? -ne 0 ]; then
    echo 'Error generating types'
    exit 1
else
    echo 'Types generated successfully'
fi