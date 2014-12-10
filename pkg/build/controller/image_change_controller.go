package controller

import (
	"github.com/golang/glog"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/client/cache"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"

	buildapi "github.com/openshift/origin/pkg/build/api"
	imageapi "github.com/openshift/origin/pkg/image/api"
)

// ImageChangeController watches for changes to ImageRepositories and triggers
// builds when a new version of a tag referenced by a BuildConfig
// is available.
type ImageChangeController struct {
	NextImageRepository func() *imageapi.ImageRepository
	BuildConfigStore    cache.Store
	BuildCreator        buildCreator
	// Stop is an optional channel that controls when the controller exits
	Stop <-chan struct{}
}

type buildCreator interface {
	CreateBuild(build *buildapi.BuildConfig, imageSubstitutions map[string]string) error
}

// Run processes ImageRepository events one by one.
func (c *ImageChangeController) Run() {
	go util.Until(c.HandleImageRepo, 0, c.Stop)
}

// HandleImageRepo processes the next ImageRepository event.
func (c *ImageChangeController) HandleImageRepo() {
	glog.V(4).Infof("Waiting for imagerepo change")
	imageRepo := c.NextImageRepository()
	glog.V(4).Infof("Build image change controller detected imagerepo change %s", imageRepo.DockerImageRepository)
	imageSubstitutions := make(map[string]string)

	for _, bc := range c.BuildConfigStore.List() {
		config := bc.(*buildapi.BuildConfig)
		glog.V(4).Infof("Detecting changed images for buildConfig %s", config.Name)

		// Extract relevant triggers for this imageRepo for this config
		var triggerForConfig *buildapi.ImageChangeTrigger
		for _, trigger := range config.Triggers {
			// for every ImageChange trigger, record the image it substitutes for and get the latest
			// image id from the imagerepository.  We will substitute all images in the buildconfig
			// with the latest values from the imagerepositories.
			if trigger.Type == buildapi.ImageChangeBuildTriggerType {
				// TODO: we don't really want to create a build for a buildconfig based the "test" tag if the "prod" tag is what just got
				// updated, but ImageRepository doesn't give us that granularity today, so the only way to avoid these spurious builds is
				// to check if the new imageid is different from the last time we built this buildcfg.  Need to add this check.
				// Will be effectively identical the logic needed on startup to spin new builds only if we missed a new image event.
				var tag string
				if tag = trigger.ImageChange.Tag; len(tag) == 0 {
					tag = buildapi.DefaultImageTag
				}
				if repoImageID, repoHasTag := imageRepo.Tags[tag]; repoHasTag {
					imageSubstitutions[trigger.ImageChange.Image] = imageRepo.DockerImageRepository + ":" + repoImageID
				}
				if trigger.ImageChange.ImageRepositoryRef.Name == imageRepo.Name {
					triggerForConfig = trigger.ImageChange
				}
			}
		}

		if triggerForConfig != nil {
			glog.V(4).Infof("Running build for buildConfig %s", config.Name)
			if err := c.BuildCreator.CreateBuild(config, imageSubstitutions); err != nil {
				glog.V(2).Infof("Error starting build for buildConfig %v: %v", config.Name, err)
			}
		}
	}
}
