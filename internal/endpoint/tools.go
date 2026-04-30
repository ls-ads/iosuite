package endpoint

// Tool is a deployable thing iosuite knows how to provision —
// real-esrgan today, future video / audio tools later.
//
// Each entry pins:
//
//   - Image: the GHCR tag iosuite will deploy. Bumped per iosuite
//     release; users always get the image this version was tested
//     against. To pin a different image at deploy-time, pass
//     --image (round 3 follow-up — not exposed in round 2 to keep
//     the surface small).
//
//   - ContainerDiskGB: image size + a few GB headroom. The trt
//     image is ~3 GB unpacked; 10 GB gives RunPod room for layer
//     cache + scratch.
//
//   - WorkersMax / IdleTimeoutS: opinionated defaults that keep
//     small users from accidentally racking up GPU bills. Override
//     via flags.
type Tool struct {
	Image           string
	ContainerDiskGB int
	WorkersMax      int
	IdleTimeoutS    int
	// Flashboot is the per-tool default for RunPod FlashBoot
	// (snapshot resume). Strong default for all known tools — the
	// images are large enough that re-pulling on every cold start
	// dominates user-visible latency. Override via --flashboot=false.
	Flashboot bool
}

// Tools is the iosuite-known catalog. New tools land here when the
// CLI gains support for them. Keep image tags pinned to a known-good
// release of the corresponding `*-serve` repo; bump in lockstep with
// iosuite's own release.
var Tools = map[string]Tool{
	"real-esrgan": {
		// Pinned to the v0.2.1 runpod-trt image — the build that
		// ships runtime/tiling.py and accepts inputs up to 4096²
		// via tile=true. See real-esrgan-serve's commit cd75cb2.
		Image:           "ghcr.io/ls-ads/real-esrgan-serve:runpod-trt-0.2.1",
		ContainerDiskGB: 10,
		WorkersMax:      2,
		IdleTimeoutS:    30,
		// FlashBoot on. The trt image is ~3 GB and the TensorRT
		// engine load takes ~30 s on top of the pull; snapshot
		// resume drops cold start from ~45 s to ~5 s.
		Flashboot: true,
	},
	// Future:
	//   "whisper":          {Image: "ghcr.io/ls-ads/whisper-serve:..."}
	//   "stable-diffusion": {Image: "ghcr.io/ls-ads/sd-serve:..."}
}

// GPUPools maps the user-facing kebab-case GPU class to the RunPod
// pool identifier used by `gpuIds` in the saveEndpoint mutation.
//
// RunPod groups physical GPUs into VRAM-and-generation pools rather
// than exposing raw model SKUs (e.g. ADA_24 covers RTX 4090, 4080,
// 4080 SUPER). Documented at https://docs.runpod.io/reference/gpu-types.
//
// Mirror real-esrgan-serve's `build/runpod_deploy.py:GPU_CLASS_TO_POOL`.
// Lift updates from there when RunPod adds new pools.
var GPUPools = map[string]string{
	"rtx-5090":    "BLACKWELL_96",
	"rtx-5080":    "BLACKWELL_96",
	"rtx-4090":    "ADA_24",
	"rtx-4080":    "ADA_24",
	"rtx-4080-s":  "ADA_24",
	"rtx-3090":    "AMPERE_24",
	"rtx-3090-ti": "AMPERE_24",
	"l4":          "AMPERE_24",
	"a5000":       "AMPERE_24",
	"l40":         "ADA_48_PRO",
	"l40s":        "ADA_48_PRO",
	"rtx-6000":    "ADA_48_PRO",
	"a40":         "AMPERE_48",
	"a6000":       "AMPERE_48",
	"a4000":       "AMPERE_16",
	"a4500":       "AMPERE_16",
	"rtx-4000":    "AMPERE_16",
	"rtx-2000":    "AMPERE_16",
	"a100":        "AMPERE_80",
	"a100-sxm":    "AMPERE_80",
	"h100":        "HOPPER_141",
	"h100-sxm":    "HOPPER_141",
	"h100-nvl":    "HOPPER_141",
	"h200":        "HOPPER_141",
	"b200":        "BLACKWELL_180",
}
