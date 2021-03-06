/*
 * Copyright 2018-2020 the original author or authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package graalvm

import (
	"fmt"

	"github.com/buildpacks/libcnb"
	"github.com/heroku/color"
	"github.com/paketo-buildpacks/libjvm"
	"github.com/paketo-buildpacks/libpak"
	"github.com/paketo-buildpacks/libpak/bard"
)

type Build struct {
	Logger bard.Logger
}

func (b Build) Build(context libcnb.BuildContext) (libcnb.BuildResult, error) {
	b.Logger.Title(context.Buildpack)
	result := libcnb.NewBuildResult()

	cr, err := libpak.NewConfigurationResolver(context.Buildpack, &b.Logger)
	if err != nil {
		return libcnb.BuildResult{}, fmt.Errorf("unable to create configuration resolver\n%w", err)
	}

	pr := libpak.PlanEntryResolver{Plan: context.Plan}

	dr, err := libpak.NewDependencyResolver(context)
	if err != nil {
		return libcnb.BuildResult{}, fmt.Errorf("unable to create dependency resolver\n%w", err)
	}

	dc, err := libpak.NewDependencyCache(context)
	if err != nil {
		return libcnb.BuildResult{}, fmt.Errorf("unable to create dependency cache\n%w", err)
	}
	dc.Logger = b.Logger

	cl := libjvm.NewCertificateLoader()
	cl.Logger = b.Logger.BodyWriter()

	v, _ := cr.Resolve("BP_JVM_VERSION")
	var nativeImage bool

	if j, ok, err := pr.Resolve("jdk"); err != nil {
		return libcnb.BuildResult{}, fmt.Errorf("unable to resolve jdk plan entry\n%w", err)
	} else if ok {
		jdkDependency, err := dr.Resolve("jdk", v)
		if err != nil {
			return libcnb.BuildResult{}, fmt.Errorf("unable to find dependency\n%w", err)
		}

		var nativeImageDependency *libpak.BuildpackDependency
		if nativeImage, ok = j.Metadata["native-image"].(bool); ok && nativeImage {
			dep, err := dr.Resolve("native-image-svm", v)
			if err != nil {
				return libcnb.BuildResult{}, fmt.Errorf("unable to find dependency\n%w", err)
			}
			nativeImageDependency = &dep
		}

		jdk, err := NewJDK(jdkDependency, nativeImageDependency, dc, cl, result.Plan)
		if err != nil {
			return libcnb.BuildResult{}, fmt.Errorf("unable to create jdk\n%w", err)
		}

		jdk.Logger = b.Logger
		result.Layers = append(result.Layers, jdk)
	}

	if e, ok, err := pr.Resolve("jre"); err != nil {
		return libcnb.BuildResult{}, fmt.Errorf("unable to resolve jre plan entry\n%w", err)
	} else if ok && !nativeImage {
		dt := libjvm.JREType
		depJRE, err := dr.Resolve("jre", v)

		if libpak.IsNoValidDependencies(err) {
			warn := color.New(color.FgYellow, color.Bold)
			b.Logger.Header(warn.Sprint("No valid JRE available, providing matching JDK instead. Using a JDK at runtime has security implications."))

			dt = libjvm.JDKType
			depJRE, err = dr.Resolve("jdk", v)
		}

		if err != nil {
			return libcnb.BuildResult{}, fmt.Errorf("unable to find dependency\n%w", err)
		}

		jre, err := libjvm.NewJRE(context.Application.Path, depJRE, dc, dt, cl, e.Metadata, result.Plan)
		if err != nil {
			return libcnb.BuildResult{}, fmt.Errorf("unable to create jre\n%w", err)
		}

		jre.Logger = b.Logger
		result.Layers = append(result.Layers, jre)

		if libjvm.IsLaunchContribution(e.Metadata) {
			helpers := []string{"active-processor-count", "java-opts", "link-local-dns", "memory-calculator",
				"openssl-certificate-loader", "security-providers-configurer"}

			if libjvm.IsBeforeJava9(depJRE.Version) {
				helpers = append(helpers, "security-providers-classpath-8")
			} else {
				helpers = append(helpers, "security-providers-classpath-9")
			}

			h := libpak.NewHelperLayerContributor(context.Buildpack, result.Plan, helpers...)
			h.Logger = b.Logger
			result.Layers = append(result.Layers, h)

			depJVMKill, err := dr.Resolve("jvmkill", "")
			if err != nil {
				return libcnb.BuildResult{}, fmt.Errorf("unable to find dependency\n%w", err)
			}

			jk := libjvm.NewJVMKill(depJVMKill, dc, result.Plan)
			jk.Logger = b.Logger
			result.Layers = append(result.Layers, jk)

			jsp := libjvm.NewJavaSecurityProperties(context.Buildpack.Info)
			jsp.Logger = b.Logger
			result.Layers = append(result.Layers, jsp)
		}
	}

	return result, nil
}
