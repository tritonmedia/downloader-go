local circle = import 'circle.libsonnet';

circle.ServiceConfig('downloader-go') {
  jobs+: {
    tests: circle.Job(dockerImage='tritonmedia/testbed', withDocker=false) {
      steps_+:: [
        // TODO(jaredallard): make our own image
        circle.RestoreCacheStep('go-deps-{{ checksum "go.sum" }}'),
        circle.RunStep('Fetch Dependencies', 'go mod vendor'),
        circle.RunStep('Run Tests', 'make test'),
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
        {
          name:: 'build',
          filters: {
            branches: {
              only: ['master'],
            },
          },
          requires: ['tests'],
        }
      ],
    },
  },
}