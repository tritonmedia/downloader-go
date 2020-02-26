local circle = import 'circle.libsonnet';
local name = 'downloader-go';

circle.ServiceConfig(name) {
  jobs+: {
    ['test-docker-build']: circle.Job() {
      steps_+:: [
        circle.BuildDockerImageStep(name),
      ],
    },
    tests: circle.Job(dockerImage='tritonmedia/testbed', withDocker=false) {
      steps_+:: [
        // TODO(jaredallard): make our own image
        circle.RestoreCacheStep('go-deps-{{ checksum "go.sum" }}'),
        circle.RunStep('Fetch Dependencies', 'go mod vendor'),
        circle.RunStep('Run Tests', 'make CGO_ENABLED=0 test'),
        circle.RunStep('Verify Go Modules', './hack/verify-go-mod.sh'),
        circle.RunStep('Verify CircleCI Configuration', './hack/verify-circleci.sh'),
        // put save_cache step here thanks to make test downloading deps...
        circle.SaveCacheStep('go-deps-{{ checksum "go.sum" }}', ['/go/pkg/mod']),
      ],
    },
  },
  workflows+: {
    ['build-push']+: {
      jobs_:: [
        'tests',
        'test-docker-build',
        {
          name:: 'build',
          filters: {
            branches: {
              only: ['master'],
            },
          },
          requires: ['tests', 'test-docker-build'],
        }
      ],
    },
  },
}