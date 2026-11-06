import os
from PIL import Image

src_path = r"C:\Users\PC\Desktop\Frame 4.png"
if not os.path.exists(src_path):
    # fallback to att path if needed
    src_path = r"C:\Users\PC\AppData\Roaming\composer\attachments\att-1793999805630-5-Frame-4.png"

img = Image.open(src_path).convert("RGBA")

# Ensure build dirs
base_dir = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
desktop_dir = os.path.join(base_dir, "desktop")

build_appicon = os.path.join(desktop_dir, "build", "appicon.png")
build_ico = os.path.join(desktop_dir, "build", "windows", "icon.ico")

# 1. 512x512 appicon
img_512 = img.resize((512, 512), Image.Resampling.LANCZOS)
img_512.save(build_appicon, "PNG")

# 2. icon.ico with multiple sizes
sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
img.save(build_ico, format="ICO", sizes=sizes)

# Also update blue-base (the default app icon)
app_icon_png = os.path.join(desktop_dir, "assets", "logo", "app-icon-blue-base.png")
img_512.save(app_icon_png, "PNG")

ico_path = os.path.join(desktop_dir, "assets", "logo", "ico", "blue-base.ico")
img.save(ico_path, format="ICO", sizes=sizes)

sized_256 = os.path.join(desktop_dir, "assets", "logo", "sized", "blue-base-256.png")
img.resize((256, 256), Image.Resampling.LANCZOS).save(sized_256, "PNG")

sized_32 = os.path.join(desktop_dir, "assets", "logo", "sized", "blue-base-32.png")
img.resize((32, 32), Image.Resampling.LANCZOS).save(sized_32, "PNG")

fe_logo = os.path.join(desktop_dir, "frontend", "src", "assets", "logo", "app-icons", "blue-base.png")
if os.path.exists(os.path.dirname(fe_logo)):
    img_512.save(fe_logo, "PNG")

print("Generated beta icons successfully!")
