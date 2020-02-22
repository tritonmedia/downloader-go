#!/usr/bin/env bash
#
# Checks if circle config was updated or not
# set -e

# make render-circle

# if git diff-index --name-status HEAD | grep .circleci/config.yml; then
#   # we succeeded, so we fail
#   git diff .circleci/config.yml
#   echo "Error: .circleci/circle.jsonnet was not rendered, or an update to the template is available"
#   echo " Run 'make render-circleci' and commit the result"
#   exit 1
# fi