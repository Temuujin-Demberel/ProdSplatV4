import struct
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from prodsplat.ply import read_gaussian_ply


def test_binary_gaussian_ply_and_sh(tmp_path: Path):
    path = tmp_path / "tiny.ply"
    props = [
        "x","y","z","f_dc_0","f_dc_1","f_dc_2",
        *[f"f_rest_{i}" for i in range(9)],
        "opacity","scale_0","scale_1","scale_2","rot_0","rot_1","rot_2","rot_3"
    ]
    header = (
        "ply\nformat binary_little_endian 1.0\n"
        "element vertex 1\n" +
        "".join(f"property float {p}\n" for p in props) +
        "end_header\n"
    ).encode()
    values = [0.0] * len(props)
    values[props.index("rot_0")] = 1.0
    path.write_bytes(header + struct.pack("<" + "f" * len(values), *values))
    ply = read_gaussian_ply(path)
    sh, degree = ply.sh_coefficients()
    assert ply.count == 1
    assert sh.shape == (1, 4, 3)
    assert degree == 1
