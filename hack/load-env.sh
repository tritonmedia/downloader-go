#!/usr/bin/env bash
# load env vars from a .env file
# must run this as source

echo "loading env variables from '.env'"
export $(grep -v '^#' .env | xargs -0)