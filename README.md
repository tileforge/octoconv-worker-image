# octoconv-worker-image

Image worker for OctoConv. One process serves one approved manifest over Worker Protocol v2.

The process reads `IMAGE_PROTOCOL_CORE_URL`, `IMAGE_PROTOCOL_CLIENT_CERT`, `IMAGE_PROTOCOL_CLIENT_KEY`, `IMAGE_PROTOCOL_CLIENT_CA`, `IMAGE_PROTOCOL_WORKER_ID`, and `IMAGE_PROTOCOL_MANIFEST_PATH`. It refuses to start when `DATABASE_URL` or `REDIS_ADDR` is set. `IMAGE_WORKER_CONCURRENCY` must be unset or `1`. Hello and pull advertise one slot, and one conversion runs at a time.

`schema/manifest-image-convert.json` is capability `octoconv.convert.image` version 1. It names the 20 ordered pairs among png, jpg, webp, heic, and tiff. `parameter_schema` is empty.

`vips` must be on `PATH`. The default engine budget is 120s (`IMAGE_ENGINE_TIMEOUT`). The output ceiling is 100 MiB. Landlock confines vips to the job directory. The read-only set is `/lib`, `/usr/lib`, `/etc/ld.so.cache`, `/proc/self/auxv`, `/proc/self/status`, `/proc/filesystems`, `/proc/cpuinfo`, `/sys/devices/system/cpu/online`, and `/sys/devices/system/cpu/possible`, plus the resolved `vips` binary. `IMAGE_SANDBOX_DISABLED=true` turns confinement off. A sandbox setup failure exits 78. Empty parameters are accepted. Any parameter key fails before vips starts. The manifest is not approved.
