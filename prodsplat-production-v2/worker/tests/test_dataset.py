from pathlib import Path
import sys
from PIL import Image, ImageDraw

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from prodsplat.dataset import build_yolo_dataset


class Guard:
    def check(self): pass
    def update(self, progress, message): pass


def test_dataset_generation(tmp_path: Path):
    renders = tmp_path / "renders"; renders.mkdir()
    backgrounds = tmp_path / "backgrounds"; backgrounds.mkdir()

    obj = Image.new("RGBA", (100, 200), (0, 0, 0, 0))
    draw = ImageDraw.Draw(obj)
    draw.rectangle((20, 10, 80, 190), fill=(255, 0, 0, 255))
    obj.save(renders / "sengur_can__az000_el+00.png")
    Image.new("RGB", (640, 480), (220, 220, 220)).save(backgrounds / "shelf.jpg")

    zip_path, count = build_yolo_dataset(str(renders), str(backgrounds), str(tmp_path / "dataset"), Guard(), copies_per_render=3)
    assert count == 3
    assert Path(zip_path).exists()
    labels = list((tmp_path / "dataset" / "labels").rglob("*.txt"))
    assert len(labels) == 3
