---
title: Build process
sidebar: documentation
permalink: documentation/internals/building_of_images/build_process.html
author: Alexey Igrychev <alexey.igrychev@flant.com>
---

werf uses the Build process to build images defined in the werf configuration.

## Dockerfile image

werf uses Dockerfile as the principal way to describe how to build an image. Images built with Dockerfile will be referred to as **dockerfile images** ([learn more about a dockerfile image]({{ "documentation/reference/werf_yaml.html#dockerfile-builder" | relative_url }})).

### How a dockerfile image is being built

werf creates a single [stage]({{ "documentation/internals/building_of_images/images_storage.html#stages" | relative_url }}) called `dockerfile` to build a dockerfile image.

How the `dockerfile` stage is being built:

 1. Stage digest is calculated based on specified `Dockerfile` and its contents. This digest represents the resulting image state.
 2. werf does not perform a new docker build if an image with this digest already exists in the [stages storage]({{ "documentation/internals/building_of_images/images_storage.html#stages-storage" | relative_url }}).
 3. werf performs a regular docker build if there is no image with the specified digest in the [stage storage]({{ "documentation/internals/building_of_images/images_storage.html#stages-storage" | relative_url }}). werf uses the standard build command of the built-in docker client (which is analogous to the `docker build` command). The local docker cache will be created and used as in the case of a regular docker client.
 4. When the docker image is complete, werf places the resulting `dockerfile` stage into the [stages storage]({{ "documentation/internals/building_of_images/images_storage.html#stages-storage" | relative_url }}) (while tagging the resulting docker image with the calculated digest) if the [`:local` stages storage]({{ "documentation/internals/building_of_images/images_storage.html#stages-storage" | relative_url }}) parameter is set.

See the [configuration article]({{ "documentation/reference/werf_yaml.html#dockerfile-builder" | relative_url }}) for the werf.yaml configuration details.

## Stapel image and artifact

Also, werf has an alternative tool for building images. The so-called stapel builder:

 * provides an integration with the git and incremental rebuilds based on the git repo history;
 * allows using ansible tasks to describe instructions needed to build an image;
 * allows sharing a common cache between builds with mounts;
 * reduces image size by detaching source data and build tools.

The image built with a stapel builder will be referred to as a **stapel image**.

See [stapel image]({{ "documentation/reference/werf_yaml.html#image-section" | relative_url }}) and [stapel artifact]({{ "documentation/advanced/building_images_with_stapel/artifacts.html" | relative_url }}) articles for more details.

### How stapel images and artifacts are built

Each stapel image or an artifact consists of several stages. The same mechanics is used to build every stage.

werf generates a specific **list of instructions** needed to build a stage. Instructions depend on the particular stage type and may contain internal service commands generated by werf along with user-specified shell commands. For example, werf may generate instructions to apply a prepared patch from a mounted text file using git cli util.

All generated instructions to build the current stage are supposed to be run in a container that is based on the previous stage. This container will be referred to as a **build container**.

werf runs instructions from the list in the build container (as you know, it is based on the previous stage). The resulting container state is then committed as a new stage and saved into the [stages storage]({{ "documentation/internals/building_of_images/images_storage.html#stages-storage" | relative_url }}).

werf has a special service image called `flant/werf-stapel`. It contains a chroot `/.werf/stapel` with all the necessary tools and libraries to build images with a stapel builder. You may find more info about the stapel image [in the article]({{ "documentation/internals/development/stapel_image.html" | relative_url }}).

`flant/werf-stapel` is mounted into every build container so that all precompiled tools are available in every stage being built and may be used in the instructions list.

### How stapel builder processes CMD and ENTRYPOINT

To build a stage image, werf launches a container with the `CMD` and `ENTRYPOINT` service parameters and then substitutes them with the [base image]({{ "documentation/advanced/building_images_with_stapel/base_image.html" | relative_url }}) values. If the base image does not have corresponding values, werf resets service to the special empty values:
* `[]` for `CMD`;
* `[""]` for `ENTRYPOINT`.

Also, werf uses the special empty value in place of a base image's `ENTRYPOINT` if a user specifies `CMD` (`docker.CMD`).

Otherwise, werf behavior is similar to [docker's](https://docs.docker.com/engine/reference/builder/#understand-how-cmd-and-entrypoint-interact).