package controller

import (
	"testing"

	kapi "github.com/GoogleCloudPlatform/kubernetes/pkg/api"

	buildapi "github.com/openshift/origin/pkg/build/api"
	buildtest "github.com/openshift/origin/pkg/build/controller/test"
	imageapi "github.com/openshift/origin/pkg/image/api"
)

type mockBuildCreator struct {
	buildcfg           *buildapi.BuildConfig
	imageSubstitutions map[string]string
}

func (m *mockBuildCreator) CreateBuild(buildcfg *buildapi.BuildConfig, imageSubstitutions map[string]string) error {
	m.buildcfg = buildcfg
	m.imageSubstitutions = imageSubstitutions
	return nil
}

func mockBuildConfig(baseImage string, triggerImage string, repoName string, repoTag string) (buildcfg *buildapi.BuildConfig) {
	buildcfg = &buildapi.BuildConfig{
		ObjectMeta: kapi.ObjectMeta{
			Name: "testBuildCfg",
		},
		Parameters: buildapi.BuildParameters{
			Strategy: buildapi.BuildStrategy{
				Type: buildapi.DockerBuildStrategyType,
				DockerStrategy: &buildapi.DockerBuildStrategy{
					ContextDir: "contextimage",
					BaseImage:  baseImage,
				},
			},
		},
		Triggers: []buildapi.BuildTriggerPolicy{
			{
				Type: buildapi.ImageChangeBuildTriggerType,
				ImageChange: &buildapi.ImageChangeTrigger{
					Image: triggerImage,
					ImageRepositoryRef: &kapi.ObjectReference{
						Name: repoName,
					},
					Tag: repoTag,
				},
			},
		},
	}
	return
}

func mockImageChangeController(buildcfg *buildapi.BuildConfig, repoName string, dockerImageRepo string, tags map[string]string) (controller *ImageChangeController) {

	imageRepo := imageapi.ImageRepository{
		ObjectMeta: kapi.ObjectMeta{
			Name: repoName,
		},
		DockerImageRepository: dockerImageRepo,
		Tags: tags,
	}

	controller = &ImageChangeController{
		NextImageRepository: func() *imageapi.ImageRepository { return &imageRepo },
		BuildConfigStore:    buildtest.NewFakeBuildConfigStore(buildcfg),
		BuildCreator:        &mockBuildCreator{},
	}
	return
}

func TestHandleImageRepo(t *testing.T) {

	// valid configuration, new build should be triggered.
	buildcfg := mockBuildConfig("registry.com/namespace/imagename", "registry.com/namespace/imagename", "testImageRepo", "test")
	controller := mockImageChangeController(buildcfg, "testImageRepo", "registry.com/namespace/imagename", map[string]string{"test": "newImageId123"})
	controller.HandleImageRepo()
	buildCreator := controller.BuildCreator.(*mockBuildCreator)
	if buildCreator.buildcfg == nil {
		t.Errorf("New build not created when new image was created")
	}
	if buildCreator.imageSubstitutions["registry.com/namespace/imagename"] != "registry.com/namespace/imagename:newImageId123" {
		t.Errorf("Image substitutions not properly setup for new build: %s |", buildCreator.imageSubstitutions["registry.com/namespace/imagename"])
	}

	// valid configuration using default latest tag, new build should be triggered.
	buildcfg = mockBuildConfig("registry.com/namespace/imagename", "registry.com/namespace/imagename", "testImageRepo", "")
	controller = mockImageChangeController(buildcfg, "testImageRepo", "registry.com/namespace/imagename", map[string]string{"latest": "newImageId123"})
	controller.HandleImageRepo()
	buildCreator = controller.BuildCreator.(*mockBuildCreator)
	if buildCreator.buildcfg == nil {
		t.Errorf("New build not created when new image was created")
	}
	if buildCreator.imageSubstitutions["registry.com/namespace/imagename"] != "registry.com/namespace/imagename:newImageId123" {
		t.Errorf("Image substitutions not properly setup for new build using default latest tag: %s |", buildCreator.imageSubstitutions["registry.com/namespace/imagename"])
	}

	// this buildconfig references a non-existent imagerepo, so an update to the real imagerepo should not
	// trigger a build here.
	buildcfg = mockBuildConfig("registry.com/namespace/imagename", "registry.com/namespace/imagename", "testImageRepo2", "test")
	controller = mockImageChangeController(buildcfg, "testImageRepo", "registry.com/namespace/imagename", map[string]string{"test": "newImageId123"})
	controller.HandleImageRepo()
	buildCreator = controller.BuildCreator.(*mockBuildCreator)
	if buildCreator.buildcfg != nil {
		t.Errorf("New build created when a different repository was updated")
	}

	// this buildconfig references a different tag than the one that will be updated, this will (for now) result
	// in a build being triggered, but there should be no image name substitution since the imagerepo does not contain
	// a valid imageid for the "test2" tag, so we should use the existing image name in the buildconfig.
	buildcfg = mockBuildConfig("registry.com/namespace/imagename", "registry.com/namespace/imagename", "testImageRepo", "test2")
	controller = mockImageChangeController(buildcfg, "testImageRepo", "registry.com/namespace/imagename", map[string]string{"test": "newImageId123"})
	controller.HandleImageRepo()
	buildCreator = controller.BuildCreator.(*mockBuildCreator)
	if buildCreator.buildcfg == nil {
		t.Errorf("New build not created when a different repository tag was updated")
	}
	if len(buildCreator.imageSubstitutions) != 0 {
		t.Errorf("Should not have had any image substitutions since tag does not exist in imagerepo")
	}
}
