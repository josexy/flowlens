# AVIF preview fixtures

These original 64 × 48 fixtures were generated with Pillow 12.2.0's libavif
encoder. Their left half is opaque red; the right half is blue, opaque in
`opaque.avif` and at alpha 128 in `alpha.avif`. They exercise decoding,
dimensions, color, and transparency without using the application's decoder
to produce its own test input.

To regenerate:

```python
from PIL import Image

for name, alpha in [("opaque", 255), ("alpha", 128)]:
    image = Image.new("RGBA", (64, 48), (255, 0, 0, 255))
    image.paste((0, 0, 255, alpha), (32, 0, 64, 48))
    image.save(f"{name}.avif", quality=100, subsampling="4:4:4", speed=6)
```
