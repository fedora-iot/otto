
APP_NAME := otto
APP_PORT := 3000

WORKSPACE := $(shell pwd)

CONTAINER_RUNTIME := $(shell command -v podman 2> /dev/null || echo docker)

.PHONY: build

build:
	${CONTAINER_RUNTIME} build -t ${APP_NAME} -f build/package/Dockerfile .

start:
	${CONTAINER_RUNTIME} run \
	-i -t --rm -p=${APP_PORT}:${APP_PORT} \
	-v ${WORKSPACE}:/source:z \
	--name="${APP_NAME}" ${APP_NAME}

stop:
	${CONTAINER_RUNTIME} stop ${APP_NAME}; ${CONTAINER_RUNTIME} rm ${APP_NAME}