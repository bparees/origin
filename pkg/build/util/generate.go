package util

import (
	"github.com/golang/glog"

	kapi "github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/openshift/origin/pkg/build/api"
)

// GenerateBuildFromConfig creates a new build based on a given BuildConfig. Optionally a SourceRevision for the new
// build can be specified
func GenerateBuildFromConfig(bc *api.BuildConfig, r *api.SourceRevision) *api.Build {
	return &api.Build{
		Parameters: api.BuildParameters{
			Source:   bc.Parameters.Source,
			Strategy: bc.Parameters.Strategy,
			Output:   bc.Parameters.Output,
			Revision: r,
		},
		ObjectMeta: kapi.ObjectMeta{
			Labels: map[string]string{api.BuildConfigLabel: bc.Name},
		},
	}
}

// GenerateBuildFromBuild creates a new build based on a given Build.
func GenerateBuildFromBuild(build *api.Build) *api.Build {
	return &api.Build{
		Parameters: build.Parameters,
		ObjectMeta: kapi.ObjectMeta{
			Labels: build.ObjectMeta.Labels,
		},
	}
}

// SubstituteImageReferences replaces references to an image with a new value
func SubstituteImageReferences(build *api.Build, oldImage string, newImage string) {
	switch {
	case build.Parameters.Strategy.Type == api.DockerBuildStrategyType &&
		build.Parameters.Strategy.DockerStrategy != nil &&
		build.Parameters.Strategy.DockerStrategy.BaseImage == oldImage:
		build.Parameters.Strategy.DockerStrategy.BaseImage = newImage
	case build.Parameters.Strategy.Type == api.STIBuildStrategyType &&
		build.Parameters.Strategy.STIStrategy != nil &&
		build.Parameters.Strategy.STIStrategy.Image == oldImage:
		build.Parameters.Strategy.STIStrategy.Image = newImage

	case build.Parameters.Strategy.Type == api.CustomBuildStrategyType:
		// update env variable references to the old image with the new image
		if build.Parameters.Strategy.CustomStrategy.Env == nil {
			build.Parameters.Strategy.CustomStrategy.Env = make([]kapi.EnvVar, 1)
			build.Parameters.Strategy.CustomStrategy.Env[0] = kapi.EnvVar{Name: api.CustomBuildStrategyBaseImageKey, Value: newImage}
		} else {
			found := false
			for i := range build.Parameters.Strategy.CustomStrategy.Env {
				glog.V(4).Infof("Checking env variable %s %s", build.Parameters.Strategy.CustomStrategy.Env[i].Name, build.Parameters.Strategy.CustomStrategy.Env[i].Value)
				if build.Parameters.Strategy.CustomStrategy.Env[i].Name == api.CustomBuildStrategyBaseImageKey {
					found = true
					if build.Parameters.Strategy.CustomStrategy.Env[i].Value == oldImage {
						build.Parameters.Strategy.CustomStrategy.Env[i].Value = newImage
						glog.V(4).Infof("Updated env variable %s %s", build.Parameters.Strategy.CustomStrategy.Env[i].Name, build.Parameters.Strategy.CustomStrategy.Env[i].Value)
						break
					}
				}
			}
			if !found {
				build.Parameters.Strategy.CustomStrategy.Env = append(build.Parameters.Strategy.CustomStrategy.Env, kapi.EnvVar{Name: api.CustomBuildStrategyBaseImageKey, Value: newImage})
			}
		}
		// update the actual custom build image with the new image, if applicable
		if build.Parameters.Strategy.CustomStrategy.Image == oldImage {
			build.Parameters.Strategy.CustomStrategy.Image = newImage
		}
	}
}
