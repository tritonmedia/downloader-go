#!/usr/bin/env bash
#
# Checks if go.sum / go.mod would be modified by a go mod tidy step
set -e

go mod tidy

if git diff-index --name-status HEAD | grep go.sum; then
  # we succeeded, so we fail
  echo "Error: go.sum was modified by a 'go mod tidy' run"
  echo " Run 'go mod tidy' and commit the result."
  exit 1
fi
