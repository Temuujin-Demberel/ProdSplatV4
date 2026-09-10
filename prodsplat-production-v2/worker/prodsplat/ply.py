from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import math
import numpy as np

PLY_DTYPES = {
    "char": "i1", "uchar": "u1", "int8": "i1", "uint8": "u1",
    "short": "<i2", "ushort": "<u2", "int16": "<i2", "uint16": "<u2",
    "int": "<i4", "uint": "<u4", "int32": "<i4", "uint32": "<u4",
    "float": "<f4", "float32": "<f4", "double": "<f8", "float64": "<f8",
}


@dataclass
class GaussianPLY:
    values: dict[str, np.ndarray]
    dropped_count: int = 0

    def require(self, name: str) -> np.ndarray:
        if name not in self.values:
            raise ValueError(f"missing PLY property: {name}")
        return self.values[name]

    @property
    def count(self) -> int:
        return len(self.require("x"))

    def means(self) -> np.ndarray:
        return np.stack([self.require("x"), self.require("y"), self.require("z")], axis=-1)

    def sh_coefficients(self) -> tuple[np.ndarray | None, int | None]:
        if not all(f"f_dc_{i}" in self.values for i in range(3)):
            return None, None
        dc = np.stack([self.require(f"f_dc_{i}") for i in range(3)], axis=-1)[:, None, :]
        rest_names = sorted(
            (name for name in self.values if name.startswith("f_rest_")),
            key=lambda name: int(name.removeprefix("f_rest_")),
        )
        if not rest_names:
            return dc.astype(np.float32), 0
        if len(rest_names) % 3 != 0:
            raise ValueError("invalid f_rest_* count; expected 3 color channels")
        per_channel = len(rest_names) // 3
        k = 1 + per_channel
        degree = int(round(math.sqrt(k) - 1))
        if (degree + 1) ** 2 != k:
            raise ValueError(f"invalid spherical-harmonic coefficient count: {k}")
        flat = np.stack([self.require(name) for name in rest_names], axis=-1)
        # Nerfstudio exports channel-major rest coefficients:
        # [R coeffs..., G coeffs..., B coeffs...].
        rest = flat.reshape(self.count, 3, per_channel).transpose(0, 2, 1)
        return np.concatenate([dc, rest], axis=1).astype(np.float32), degree


def read_gaussian_ply(path: str | Path) -> GaussianPLY:
    path = Path(path)
    with path.open("rb") as f:
        if f.readline().decode("ascii", "strict").strip() != "ply":
            raise ValueError("not a PLY file")
        fmt = None
        vertex_count = None
        props: list[tuple[str, str]] = []
        in_vertex = False
        header_lines = 1

        while True:
            raw = f.readline()
            if not raw:
                raise ValueError("unexpected EOF in PLY header")
            header_lines += 1
            if header_lines > 4096:
                raise ValueError("PLY header is unreasonably large")
            line = raw.decode("ascii", "strict").strip()
            parts = line.split()
            if not parts:
                continue
            if parts[0] == "format":
                fmt = parts[1]
            elif parts[0] == "element":
                in_vertex = parts[1] == "vertex"
                if in_vertex:
                    vertex_count = int(parts[2])
            elif parts[0] == "property" and in_vertex:
                if parts[1] == "list":
                    raise ValueError("list properties are not supported for Gaussian vertices")
                if parts[1] not in PLY_DTYPES:
                    raise ValueError(f"unsupported PLY property type: {parts[1]}")
                props.append((parts[2], parts[1]))
            elif line == "end_header":
                break

        if vertex_count is None or vertex_count <= 0:
            raise ValueError("missing/empty PLY vertex element")
        if vertex_count > 100_000_000:
            raise ValueError("PLY vertex count exceeds safety limit")

        names = [name for name, _ in props]
        required = ["x", "y", "z", "opacity", "scale_0", "scale_1", "scale_2",
                    "rot_0", "rot_1", "rot_2", "rot_3"]
        missing = [name for name in required if name not in names]
        if missing:
            raise ValueError(f"not a Gaussian PLY; missing {missing}")

        if fmt == "binary_little_endian":
            dtype = np.dtype([(name, PLY_DTYPES[typ]) for name, typ in props])
            arr = np.fromfile(f, dtype=dtype, count=vertex_count)
            if len(arr) != vertex_count:
                raise ValueError("truncated binary PLY")
            values = {name: arr[name].astype(np.float32, copy=False) for name, _ in props}
        elif fmt == "ascii":
            data = np.loadtxt(f, dtype=np.float64, max_rows=vertex_count)
            if data.ndim == 1:
                data = data[None, :]
            if data.shape[0] != vertex_count or data.shape[1] < len(props):
                raise ValueError("truncated ASCII PLY")
            values = {name: data[:, i].astype(np.float32) for i, (name, _) in enumerate(props)}
        else:
            raise ValueError(f"unsupported PLY format: {fmt}")

    finite = np.ones(vertex_count, dtype=bool)
    for key in required:
        finite &= np.isfinite(values[key])
    if not finite.any():
        raise ValueError("every Gaussian has a non-finite value in a required property")
    dropped = int(vertex_count - finite.sum())
    if dropped:
        values = {name: array[finite] for name, array in values.items()}
    return GaussianPLY(values, dropped)


def write_gaussian_ply(values: dict[str, np.ndarray], path: str | Path) -> None:
    path = Path(path)
    names = list(values)
    count = len(values[names[0]])
    header = (
        "ply\nformat binary_little_endian 1.0\n"
        f"element vertex {count}\n"
        + "".join(f"property float {name}\n" for name in names)
        + "end_header\n"
    )
    table = np.empty(count, dtype=np.dtype([(name, "<f4") for name in names]))
    for name in names:
        table[name] = np.asarray(values[name], dtype=np.float32)
    temp = path.with_name(path.name + ".partial")
    with temp.open("wb") as f:
        f.write(header.encode("ascii"))
        table.tofile(f)
    temp.replace(path)
