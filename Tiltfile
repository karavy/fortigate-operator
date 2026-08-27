allow_k8s_contexts('kind-kind')

local_resource(
    'go-build',
    cmd='mkdir -p ./bin && GOOS=linux GOARCH=amd64 go build -o ./bin/manager ./cmd/main.go',
    deps=['cmd', 'internal', 'go.mod', 'go.sum'],
)

local_resource(
    'go-build-agent',
    cmd='mkdir -p ./bin && GOOS=linux GOARCH=amd64 go build -o ./bin/agent ./cmd/agent/main.go',
    deps=['cmd', 'internal', 'go.mod', 'go.sum'],
)

docker_build(
    'cdmdckr/redis-operator',
    '.',
    dockerfile='Dockerfile.dev.operator',
    only=[
        'Dockerfile.dev.operator',
        'terraform/',
        'bin/manager',
    ],
    live_update=[
        sync('./bin/manager', '/manager'),
        run('killall manager || true'),
    ],
)

docker_build(
    'cdmdckr/network-operator',
    '.',
    dockerfile='Dockerfile.dev.agent',
    only=[
        'Dockerfile.dev.agent',
        'bin/agent',
    ],
    live_update=[
        sync('./bin/agent', '/agent'),
        run('killall agent || true'),
    ],
)

k8s_yaml(kustomize('config/default'))
k8s_resource('fgt-operator', port_forwards=8085)

k8s_yaml(kustomize('config/agent'))
k8s_resource('network-operator-agent', resource_deps=['go-build-agent'])