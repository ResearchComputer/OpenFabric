"""
Plot utilities for scientific paper figures.

This module provides consistent styling and utilities for creating publication-quality
figures, with default support for double-column formats commonly used in scientific papers.
"""

import warnings
import numpy as np
import seaborn as sns
import matplotlib as mpl
from matplotlib import rcParams
import matplotlib.pyplot as plt
from typing import Any, Callable, Dict, List, Optional, Tuple, Union
from matplotlib.ticker import FuncFormatter, LogLocator, NullFormatter

# Figure size configurations for common scientific paper formats
# Widths are in inches, typical for scientific publications
FIGURE_SIZES = {
    # Double column formats (default)
    "double_column": {
        "width": 9,  # 3.375 inches per column * 2 columns (standard for many journals)
        "height": 3.75,  # Golden ratio approximation
    },
    # Single column formats
    "single_column": {
        "width": 3.375,  # Standard single column width
        "height": 2.75,
        "aspect_ratio": 0.81,
    },
    # Extended figures
    "double_column_wide": {"width": 7.0, "height": 5.0, "aspect_ratio": 0.71},
    # Square figures
    "double_column_square": {"width": 6.75, "height": 6.75, "aspect_ratio": 1.0},
}

# Default publication style parameters
DEFAULT_STYLE = {
    "font.family": "serif",
    "font.serif": ["Times New Roman", "DejaVu Serif", "Computer Modern Roman"],
    "font.size": 8,  # Base font size for double column figures
    "axes.labelsize": 8,
    "axes.titlesize": 9,
    "xtick.labelsize": 7,
    "ytick.labelsize": 7,
    "legend.fontsize": 7,
    "figure.titlesize": 10,
    "lines.linewidth": 1.0,
    "axes.linewidth": 0.8,
    "grid.linewidth": 0.5,
    "axes.spines.left": True,
    "axes.spines.bottom": True,
    "axes.spines.top": False,
    "axes.spines.right": False,
    "axes.grid": False,
    "grid.alpha": 0.3,
    "legend.framealpha": 0.9,
    "legend.fancybox": False,
    "legend.edgecolor": "black",
    "legend.frameon": True,
    "figure.dpi": 300,
    "savefig.dpi": 300,
    "savefig.bbox": "tight",
    "savefig.pad_inches": 0.1,
}

# Color palettes for scientific publications
COLOR_PALETTES = {
    "basic": [
        "",
        "",
    ],
    "default": [
  "#E69F00",
  "#88CCEE",
  "#BBBBBB",
  "#117733",
  "#0000ff",
  "#DDCC77",
  "#CC6677",
  "#D55E00",
  "#AA4499",
  "#4477AA",

  "#EE6677",
  "#228833",
  "#CCBB44",
  "#66CCEE",
  "#AA3377",
  "#BBBBBB",

  "#E69F00",
  "#56B4E9",
  "#009E73",
  "#F0E442",
  "#0072B2",
  "#D55E00",
  "#CC79A7",
  "#000000"
    ],
    "colorblind": [
        "#0173b2",
        "#de8f05",
        "#029e73",
        "#cc78bc",
        "#ca9161",
        "#fbafe4",
        "#949494",
        "#ece133",
        "#56b4e9",
        "#009e73",
    ],
    "grayscale": [
        "#000000",
        "#404040",
        "#808080",
        "#a0a0a0",
        "#c0c0c0",
        "#606060",
        "#909090",
        "#b0b0b0",
        "#d0d0d0",
        "#e0e0e0",
    ],
    "high_contrast": [
        "#000000",
        "#ffffff",
        "#ff0000",
        "#00ff00",
        "#0000ff",
        "#ffff00",
        "#ff00ff",
        "#00ffff",
        "#808080",
        "#c0c0c0",
    ],
    "okabe_ito": [
        "#000000",
        "#E69F00",
        "#56B4E9",
        "#009E73",
        "#F0E442",
        "#0072B2",
        "#D55E00",
        "#CC79A7",
    ],
    "glasbey": [
        "#FF0000",
        "#00FF00",
        "#0000FF",
        "#FFFF00",
        "#FF00FF",
        "#00FFFF",
        "#800000",
        "#008000",
        "#000080",
        "#808000",
    ],
    "bold": [
        "#0D1B2A",
        "#F94144",
        "#F3722C",
        "#F9C74F",
        "#90BE6D",
        "#43AA8B",
        "#577590",
        "#277DA1",
    ],
    "contrast_dark": [
        "#F8F9FA",
        "#FFD166",
        "#06D6A0",
        "#118AB2",
        "#EF476F",
        "#8338EC",
        "#FF006E",
        "#3A86FF",
    ],
}

# Marker styles for different plot types
MARKER_STYLES = {
    "default_markers": ["o", "s", "^", "v", "D", "<", ">", "p", "*", "h"],
    "open_markers": ["o", "s", "^", "v", "D", "<", ">", "p", "*", "h"],
    "filled_markers": ["o", "s", "^", "v", "D", "<", ">", "p", "*", "h"],
}

# Line styles
LINE_STYLES = ["-", "--", "-.", ":", (0, (3, 1, 1, 1)), (0, (5, 1, 1, 1))]


def set_publication_style(
    font_size: Optional[int] = None,
    family: Optional[str] = None,
    use_latex: bool = False,
    dpi: Optional[int] = None,
    style_overrides: Optional[Dict[str, Any]] = None,
) -> None:
    """
    Set matplotlib parameters for publication-quality figures.

    Parameters
    ----------
    font_size : int, optional
        Base font size. Default is 8 for double column figures.
    family : str, optional
        Font family ('serif', 'sans-serif', 'monospace')
    use_latex : bool, default False
        Use LaTeX for text rendering. Requires LaTeX installation.
    dpi : int, optional
        Figure DPI. Default is 300.
    style_overrides : dict, optional
        Arbitrary rcParams to override built-in defaults (e.g., {'axes.grid': True}).
    """
    style = DEFAULT_STYLE.copy()

    if font_size is not None:
        style["font.size"] = font_size
        style["axes.labelsize"] = font_size
        style["axes.titlesize"] = font_size + 1
        style["xtick.labelsize"] = font_size - 1
        style["ytick.labelsize"] = font_size - 1
        style["legend.fontsize"] = font_size - 1
        style["figure.titlesize"] = font_size + 2

    if family is not None:
        style["font.family"] = family
        if family == "serif":
            style["font.serif"] = [
                "Times New Roman",
                "DejaVu Serif",
                "Computer Modern Roman",
            ]
        elif family == "sans-serif":
            style["font.sans-serif"] = ["Arial", "Helvetica", "DejaVu Sans"]

    if dpi is not None:
        style["figure.dpi"] = dpi
        style["savefig.dpi"] = dpi

    if use_latex:
        style["text.usetex"] = True
        style["font.family"] = "serif"
        warnings.warn("LaTeX rendering requires a working LaTeX installation.")

    # Apply the style
    if style_overrides:
        style.update(style_overrides)
    for key, value in style.items():
        rcParams[key] = value
    sns.set_style("ticks")
    font = {
        "font.size": 12,
    }
    sns.set_style(font)
    paper_rc = {
        "lines.linewidth": 3,
        "lines.markersize": 10,
    }
    sns.set_context("paper", font_scale=2, rc=paper_rc)
    mpl.rcParams["pdf.fonttype"] = 42
    mpl.rcParams["ps.fonttype"] = 42
    
def set_default_plot_settings(
    max_columns: int = 500,
    seaborn_style: str = "ticks",
    font_family: str = "Roboto",
    font_size: int = 12,
    context: str = "paper",
    font_scale: float = 2.0,
    linewidth: float = 3.0,
    markersize: float = 10.0,
    palette_name: str = "tab10",
    palette_size: Optional[int] = None,
    pdf_fonttype: int = 42,
    ps_fonttype: int = 42,
    seaborn_rc_overrides: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    """Configure commonly used plotting defaults built on seaborn.

    The configuration mirrors a set of defaults frequently used for paper-ready
    plots that rely on pandas and seaborn utilities. The function returns a
    dictionary containing convenient handles (palette, line styles, grid
    parameters) that can be re-used by callers when building figures.

    Parameters
    ----------
    max_columns : int, default 500
        Maximum number of columns displayed by pandas when printing DataFrames.
    seaborn_style : str, default 'ticks'
        Base seaborn style passed to ``sns.set_theme``.
    font_family : str, default 'Roboto'
        Primary font family requested from seaborn/matplotlib.
    font_size : int, default 12
        Base font size registered with seaborn's rc configuration.
    context : str, default 'paper'
        Seaborn plot context (e.g., 'paper', 'talk', 'poster').
    font_scale : float, default 2.0
        Global font scaling factor applied by seaborn.
    linewidth : float, default 3.0
        Default line width injected into seaborn rc parameters.
    markersize : float, default 10.0
        Default marker size injected into seaborn rc parameters.
    palette_name : str, default 'tab10'
        Name of the palette passed to ``sns.color_palette``.
    palette_size : int, optional
        Optional number of discrete colors requested from seaborn.
    pdf_fonttype : int, default 42
        Matplotlib PDF font type (42 embeds TrueType fonts).
    ps_fonttype : int, default 42
        Matplotlib PostScript font type.
    seaborn_rc_overrides : dict, optional
        Additional RC parameters merged into the seaborn configuration.

    Returns
    -------
    dict
        A dictionary with keys ``palette``, ``linestyles``, and ``grid_params``.
    """

    try:
        import pandas as pd
        import seaborn as sns
    except ImportError as exc:  # pragma: no cover - optional dependency guard
        raise ImportError(
            "set_default_plot_settings requires pandas and seaborn to be installed."
        ) from exc

    pd.set_option("display.max_columns", max_columns)

    rc_updates = {
        "lines.linewidth": linewidth,
        "lines.markersize": markersize,
        "font.family": font_family,
        "font.size": font_size,
    }
    if seaborn_rc_overrides:
        rc_updates.update(seaborn_rc_overrides)

    sns.set_theme(
        style=seaborn_style,
        context=context,
        font=font_family,
        font_scale=font_scale,
        rc=rc_updates,
    )

    palette = sns.color_palette(palette_name, palette_size)

    mpl.rcParams["pdf.fonttype"] = pdf_fonttype
    mpl.rcParams["ps.fonttype"] = ps_fonttype

    return {
        "palette": palette,
        "linestyles": ["-", "--", ":", ":", ":"],
        "grid_params": {"width_ratios": [1, 1]},
    }


def get_figure_size(
    format_type: str = "double_column",
    height_ratio: Optional[float] = None,
    n_panels: int = 1,
    n_cols: int = 1,
    width: Optional[float] = None,
    panel_scale_factor: float = 0.9,
    col_scale_factor: float = 1.2,
) -> Tuple[float, float]:
    """
    Calculate figure size based on publication format.

    Parameters
    ----------
    format_type : str, default 'double_column'
        Type of figure format ('double_column', 'single_column', 'double_column_wide', etc.)
    height_ratio : float, optional
        Custom height-to-width ratio. If None, uses default for format_type.
    n_panels : int, default 1
        Number of subplot panels (approximate height adjustment)
    n_cols : int, default 1
        Number of subplot columns

    Additional Parameters
    ---------------------
    width : float, optional
        Override width in inches (bypasses format preset width when provided).
    panel_scale_factor : float, default 0.9
        Multiplier applied to height when stacking multiple panels.
    col_scale_factor : float, default 1.2
        Multiplier applied to height when using multiple columns.

    Returns
    -------
    width, height : tuple of float
        Figure dimensions in inches.
    """
    if format_type not in FIGURE_SIZES:
        raise ValueError(
            f"Unknown format type: {format_type}. Available: {list(FIGURE_SIZES.keys())}"
        )

    config = FIGURE_SIZES[format_type]
    width = config["width"] if width is None else width

    if height_ratio is None:
        height_ratio = config["aspect_ratio"]

    # Adjust height for multiple panels
    height = width * height_ratio

    # Scale height for multiple panels/columns
    if n_cols > 1:
        height *= col_scale_factor  # Add space for column layout
    if n_panels > 1:
        height *= (
            np.sqrt(n_panels) * panel_scale_factor
        )  # Scale with sqrt of panel count

    return width, height


def create_figure(
    format_type: str = "double_column",
    nrows: int = 1,
    ncols: int = 1,
    height_ratio: Optional[float] = None,
    figsize: Optional[Tuple[float, float]] = None,
    **kwargs,
) -> Tuple[plt.Figure, Union[plt.Axes, np.ndarray]]:
    """
    Create a figure with publication-standard dimensions.

    Parameters
    ----------
    format_type : str, default 'double_column'
        Figure format type
    nrows : int, default 1
        Number of subplot rows
    ncols : int, default 1
        Number of subplot columns
    height_ratio : float, optional
        Custom height-to-width ratio
    **kwargs
        Additional arguments passed to plt.subplots()

    Additional Parameters
    ---------------------
    figsize : (float, float), optional
        Explicit figure size to use (skips get_figure_size when provided).

    Returns
    -------
    fig, axes : tuple
        Figure and axes objects.
    """
    # Calculate figure size unless explicitly provided
    if figsize is None:
        n_panels = nrows * ncols
        width, height = get_figure_size(format_type, height_ratio, n_panels, ncols)
        figsize = (width, height)
    if figsize == "double_column":
        ncols = 2
        nrows = 1
    # Set default figure parameters if not provided
    subplot_kwargs = {
        "figsize": figsize,
        "dpi": rcParams.get("figure.dpi", 300),
        "constrained_layout": True,
    }
    subplot_kwargs.update(kwargs)

    fig, axes = plt.subplots(nrows=nrows, ncols=ncols, **subplot_kwargs)

    return fig, axes


def create_default_side_by_side_figure(
    figsize: Tuple[float, float] = (9.0, 3.75),
    constrained_layout: bool = True,
    ncols: int = 2,
    nrows: int = 1,
    **kwargs,
) -> Tuple[plt.Figure, Union[plt.Axes, np.ndarray]]:
    """Create a two-panel figure with defaults matching the seaborn preset."""

    subplot_kwargs = {
        "figsize": figsize,
        "constrained_layout": constrained_layout,
        "ncols": ncols,
        "nrows": nrows,
    }
    subplot_kwargs.update(kwargs)

    fig, axes = plt.subplots(**subplot_kwargs)

    return fig, axes


def apply_color_palette(
    palette_name: str = "default",
    n_colors: Optional[int] = None,
    palettes: Optional[Dict[str, List[str]]] = None,
    mode: str = "repeat",
    start_index: int = 0,
) -> list:
    """
    Get a color palette for plotting.

    Parameters
    ----------
    palette_name : str, default 'default'
        Name of color palette. Options include 'default', 'colorblind',
        'grayscale', 'high_contrast', 'okabe_ito', 'glasbey', 'bold',
        'contrast_dark'.
    n_colors : int, optional
        Number of colors to return. If None, returns all colors in palette.
    palettes : dict[str, list[str]], optional
        Custom palette registry to override or extend built-ins.
    mode : {'repeat','truncate'}, default 'repeat'
        If n_colors exceeds palette length, either repeat or truncate.
    start_index : int, default 0
        Starting index into the palette before cycling/truncating.

    Returns
    -------
    colors : list
        List of color hex codes.
    """
    registry = {**COLOR_PALETTES}
    if palettes:
        registry.update(palettes)
    if palette_name not in registry:
        raise ValueError(
            f"Unknown palette: {palette_name}. Available: {list(registry.keys())}"
        )

    colors = registry[palette_name]
    if start_index:
        colors = colors[start_index:] + colors[:start_index]

    if n_colors is not None:
        if mode == "repeat":
            # Cycle through colors if needed
            n_full_cycles = n_colors // len(colors)
            n_remaining = n_colors % len(colors)
            colors = colors * n_full_cycles + colors[:n_remaining]
        elif mode == "truncate":
            colors = colors[:n_colors]
        else:
            raise ValueError("mode must be either 'repeat' or 'truncate'")

    return colors


def style_axes(
    ax: plt.Axes,
    title: Optional[str] = None,
    xlabel: Optional[str] = None,
    ylabel: Optional[str] = None,
    xlim: Optional[Tuple[float, float]] = None,
    ylim: Optional[Tuple[float, float]] = None,
    grid: bool = False,
    spine_style: str = "default",
    legend: bool = False,
    legend_kwargs: Optional[Dict[str, Any]] = None,
    grid_alpha: Optional[float] = None,
    grid_linewidth: Optional[float] = None,
    minimal_spine_offset: int = 10,
    legend_style: Optional[Dict[str, Any]] = None,
    **kwargs,
) -> plt.Axes:
    """
    Apply consistent styling to axes.

    Parameters
    ----------
    ax : matplotlib.axes.Axes
        Axes object to style
    title : str, optional
        Axes title
    xlabel : str, optional
        X-axis label
    ylabel : str, optional
        Y-axis label
    xlim : tuple, optional
        X-axis limits
    ylim : tuple, optional
        Y-axis limits
    grid : bool, default False
        Whether to show grid
    spine_style : str, default 'default'
        Spine style ('default', 'box', 'minimal', 'none')
    legend : bool, default False
        Whether to show legend
    legend_kwargs : dict, optional
        Additional keyword arguments forwarded to ax.legend()
    **kwargs
        Additional styling arguments

    Returns
    -------
    ax : matplotlib.axes.Axes
        Styled axes object.
    """
    # Set labels and title
    if title is not None:
        ax.set_title(title, pad=10)
    if xlabel is not None:
        ax.set_xlabel(xlabel, labelpad=5)
    if ylabel is not None:
        ax.set_ylabel(ylabel, labelpad=5)

    # Set limits
    if xlim is not None:
        ax.set_xlim(xlim)
    if ylim is not None:
        ax.set_ylim(ylim)

    # Configure spines
    if spine_style == "default":
        ax.spines["top"].set_visible(False)
        ax.spines["right"].set_visible(False)
    elif spine_style == "box":
        ax.spines["top"].set_visible(True)
        ax.spines["right"].set_visible(True)
    elif spine_style == "minimal":
        ax.spines["top"].set_visible(False)
        ax.spines["right"].set_visible(False)
        ax.spines["left"].set_position(("outward", minimal_spine_offset))
        ax.spines["bottom"].set_position(("outward", minimal_spine_offset))
    elif spine_style == "none":
        for spine in ax.spines.values():
            spine.set_visible(False)

    # Configure grid
    if grid:
        ax.grid(
            True,
            alpha=0.3 if grid_alpha is None else grid_alpha,
            linewidth=0.5 if grid_linewidth is None else grid_linewidth,
        )

    # Configure legend
    legend_obj = None
    if legend:
        handles, labels = ax.get_legend_handles_labels()
        if handles:
            legend_params = dict(legend_kwargs or {})
            legend_obj = ax.legend(**legend_params)

    if legend_obj is None:
        legend_obj = ax.get_legend()

    if legend_obj is not None:
        # Defaults mirror the previous behavior; allow overrides via legend_style
        frame_on = True if legend_style is None else legend_style.get("frameon", True)
        edgecolor = (
            "black" if legend_style is None else legend_style.get("edgecolor", "black")
        )
        fancybox = (
            False if legend_style is None else legend_style.get("fancybox", False)
        )
        legend_obj.set_frame_on(frame_on)
        frame = getattr(legend_obj, "get_frame", lambda: None)()
        if frame is not None and hasattr(frame, "set_edgecolor"):
            frame.set_edgecolor(edgecolor)
        if hasattr(legend_obj, "set_fancybox"):
            legend_obj.set_fancybox(fancybox)

    # Apply additional kwargs
    for key, value in kwargs.items():
        if hasattr(ax, f"set_{key}"):
            getattr(ax, f"set_{key}")(value)

    return ax


def save_figure(
    fig: plt.Figure,
    filename: str,
    formats: Optional[list] = None,
    dpi: Optional[int] = None,
    bbox_inches: Optional[str] = None,
    pad_inches: Optional[float] = None,
    transparent: Optional[bool] = None,
    **kwargs,
) -> None:
    """
    Save figure in publication-quality formats.

    Parameters
    ----------
    fig : matplotlib.figure.Figure
        Figure object to save
    filename : str
        Base filename (without extension)
    formats : list, optional
        List of formats to save. Default is ['pdf', 'png']
    **kwargs
        Additional arguments passed to fig.savefig()
    """
    if formats is None:
        formats = ["pdf", "png"]

    save_kwargs = {
        "dpi": rcParams.get("savefig.dpi", 300) if dpi is None else dpi,
        "bbox_inches": "tight" if bbox_inches is None else bbox_inches,
        "pad_inches": 0.1 if pad_inches is None else pad_inches,
        "transparent": False if transparent is None else transparent,
    }
    save_kwargs.update(kwargs)

    for fmt in formats:
        full_filename = f"{filename}.{fmt}"
        fig.savefig(full_filename, format=fmt, **save_kwargs)
        print(f"Saved figure: {full_filename}")


# Convenience functions for common plot types
def plot_line(
    x: np.ndarray,
    y: np.ndarray,
    ax: Optional[plt.Axes] = None,
    color: Optional[str] = None,
    linestyle: str = "-",
    linewidth: float = 1.0,
    marker: Optional[str] = None,
    label: Optional[str] = None,
    palette_name: str = "default",
    **kwargs,
) -> plt.Axes:
    """
    Create a line plot with consistent styling.

    Additional Parameters
    ---------------------
    palette_name : str, default 'default'
        Palette to draw a color from when color is not provided.
    """
    if ax is None:
        fig, ax = create_figure()

    if color is None:
        colors = apply_color_palette(palette_name, 1)
        color = colors[0]

    ax.plot(
        x,
        y,
        color=color,
        linestyle=linestyle,
        linewidth=linewidth,
        marker=marker,
        label=label,
        **kwargs,
    )

    return ax


def plot_scatter(
    x: np.ndarray,
    y: np.ndarray,
    ax: Optional[plt.Axes] = None,
    color: Optional[str] = None,
    marker: str = "o",
    s: float = 20,
    alpha: float = 0.7,
    label: Optional[str] = None,
    palette_name: str = "default",
    **kwargs,
) -> plt.Axes:
    """
    Create a scatter plot with consistent styling.

    Additional Parameters
    ---------------------
    palette_name : str, default 'default'
        Palette to draw a color from when color is not provided.
    """
    if ax is None:
        fig, ax = create_figure()

    if color is None:
        colors = apply_color_palette(palette_name, 1)
        color = colors[0]

    ax.scatter(
        x, y, color=color, marker=marker, s=s, alpha=alpha, label=label, **kwargs
    )

    return ax


def plot_errorbar(
    x: np.ndarray,
    y: np.ndarray,
    yerr: Optional[np.ndarray] = None,
    xerr: Optional[np.ndarray] = None,
    ax: Optional[plt.Axes] = None,
    color: Optional[str] = None,
    linestyle: str = "-",
    linewidth: float = 1.0,
    marker: Optional[str] = None,
    capsize: float = 3,
    label: Optional[str] = None,
    palette_name: str = "default",
    **kwargs,
) -> plt.Axes:
    """
    Create an error bar plot with consistent styling.

    Additional Parameters
    ---------------------
    palette_name : str, default 'default'
        Palette to draw a color from when color is not provided.
    """
    if ax is None:
        fig, ax = create_figure()

    if color is None:
        colors = apply_color_palette(palette_name, 1)
        color = colors[0]

    ax.errorbar(
        x,
        y,
        yerr=yerr,
        xerr=xerr,
        color=color,
        linestyle=linestyle,
        linewidth=linewidth,
        marker=marker,
        capsize=capsize,
        label=label,
        **kwargs,
    )

    return ax


def plot_stacked_area(
    x: np.ndarray,
    y_data: list,
    labels: list,
    ax: Optional[plt.Axes] = None,
    colors: Optional[list] = None,
    alpha: float = 0.8,
    baseline: Optional[np.ndarray] = None,
    **kwargs,
) -> plt.Axes:
    """
    Create a stacked area plot with consistent styling.

    Parameters
    ----------
    x : np.ndarray
        X-axis data (typically time series)
    y_data : list of np.ndarray
        List of y-axis data arrays for each layer
    labels : list of str
        Labels for each layer
    ax : matplotlib.axes.Axes, optional
        Axes object to plot on
    colors : list, optional
        Colors for each layer
    alpha : float, default 0.8
        Transparency of the areas
    baseline : np.ndarray, optional
        Starting baseline for stacking (defaults to zeros).
    **kwargs
        Additional arguments passed to ax.fill_between()

    Returns
    -------
    ax : matplotlib.axes.Axes
        Axes object with the stacked area plot.
    """
    if ax is None:
        fig, ax = create_figure()

    if colors is None:
        colors = apply_color_palette("default", len(y_data))

    if len(y_data) != len(labels):
        raise ValueError("Length of y_data must match length of labels")

    # Convert to numpy arrays if needed
    y_data = [np.asarray(y) for y in y_data]

    # Create stacked area plot
    bottom = (
        np.zeros_like(x, dtype=float)
        if baseline is None
        else np.asarray(baseline, dtype=float)
    )

    for i, (y, label, color) in enumerate(zip(y_data, labels, colors)):
        ax.fill_between(
            x, bottom, bottom + y, color=color, alpha=alpha, label=label, **kwargs
        )
        bottom += y

    return ax


def plot_stacked_area_plot(
    df,
    x_col: str,
    y_columns: list,
    ax: Optional[plt.Axes] = None,
    colors: Optional[list] = None,
    label_map: Optional[Dict[str, str]] = None,
    label_formatter: Optional[Callable[[str], str]] = None,
    title: str = "",
    xlabel: str = "",
    ylabel: str = "",
    format_type: str = "double_column_wide",
    legend: bool = True,
    legend_kwargs: Optional[Dict[str, Any]] = None,
    legend_offset: float = -0.25,
    sort_x: bool = False,
    x_tick_formatter: Optional[Callable[[Any, int], str]] = None,
    max_ticks: int = 10,
    x_tick_rotation: int = 45,
    palette_name: str = "colorblind",
    legend_ncol: Optional[int] = None,
    datetime_threshold_short_hours: float = 24.0,
    datetime_threshold_medium_hours: float = 24.0 * 7,
    datetime_formats: Optional[Tuple[str, str, str]] = None,
    legend_baseline_offset: float = 0.25,
    subplot_bottom_base: float = 0.25,
    min_extra_margin: float = 0.0,
    **kwargs,
) -> plt.Axes:
    """
    Create a publication-ready stacked area chart from a DataFrame.

    Parameters
    ----------
    df : pandas.DataFrame
        Source data containing the x-axis column and the stacked series.
    x_col : str
        Column name to use for the x-axis.
    y_columns : list of str
        Columns that form the stacked series.
    ax : matplotlib.axes.Axes, optional
        Axes object to draw on. Created automatically when omitted.
    colors : list, optional
        Colors for each stacked series. Defaults to the colorblind palette.
    label_map : dict, optional
        Mapping from column name to legend label.
    label_formatter : callable, optional
        Function applied to each column name when generating legend labels.
    title, xlabel, ylabel : str, optional
        Figure annotations applied via `style_axes`.
    format_type : str, default 'double_column_wide'
        Figure sizing preset used when creating a new axis.
    legend : bool, default True
        Whether to draw a legend for the stacked series.
    legend_kwargs : dict, optional
        Extra keyword arguments forwarded to `ax.legend`.
    legend_offset : float, default -0.25
        Vertical offset (in axes fraction coordinates) for the legend anchor.
    sort_x : bool, default False
        Sort the DataFrame by `x_col` before plotting.
    x_tick_formatter : callable, optional
        Function to convert raw x values into tick labels.
    max_ticks : int, default 10
        Maximum number of ticks shown on the x-axis.
    x_tick_rotation : int, default 45
        Rotation applied to x tick labels.
    **kwargs
        Extra options forwarded to `plot_stacked_area`.

    Returns
    -------
    ax : matplotlib.axes.Axes
        Axes containing the stacked area chart.
    """
    import pandas as pd

    if ax is None:
        fig, ax = create_figure(format_type=format_type)

    if x_col not in df.columns:
        raise ValueError(f"x_col '{x_col}' not found in DataFrame")

    if not y_columns:
        raise ValueError("y_columns must contain at least one column name")

    # Work on a copy so we can safely sort or drop invalid rows.
    data = df.copy()
    if sort_x:
        data = data.sort_values(x_col)

    data = data.loc[data[x_col].notna()]
    if data.empty:
        raise ValueError("No rows remaining after dropping missing x values.")

    x_series = pd.Series(data[x_col]).reset_index(drop=True)

    # Assemble stacked series arrays and human-friendly labels.
    y_data = []
    labels = []
    for col in y_columns:
        if col not in data.columns:
            raise ValueError(f"y_column '{col}' not found in DataFrame")
        series = pd.to_numeric(data[col], errors="coerce").fillna(0.0)
        y_data.append(series.to_numpy())
        if label_map and col in label_map:
            label = label_map[col]
        elif label_formatter is not None:
            label = label_formatter(col)
        else:
            label = col
        labels.append(label)

    if colors is None:
        colors = apply_color_palette(palette_name, len(y_data))

    is_datetime = pd.api.types.is_datetime64_any_dtype(x_series)
    is_numeric = np.issubdtype(x_series.dtype, np.number)

    if is_datetime:
        x_values = pd.to_datetime(x_series)
        x_numeric = np.arange(len(x_values))
    elif is_numeric:
        x_values = x_series.astype(float)
        x_numeric = x_values.to_numpy()
    else:
        x_values = x_series.astype(str)
        x_numeric = np.arange(len(x_values))

    ax = plot_stacked_area(x_numeric, y_data, labels, ax=ax, colors=colors, **kwargs)

    # Derive x-axis tick labels that match the data type (numeric vs time).
    tick_count = min(max_ticks, len(x_series)) if len(x_series) else 0
    if tick_count:
        tick_indices = np.linspace(0, len(x_series) - 1, tick_count, dtype=int)
        if x_tick_formatter is not None:
            tick_labels = [
                x_tick_formatter(x_series.iloc[idx], idx) for idx in tick_indices
            ]
        elif is_datetime:
            x_dt = pd.to_datetime(x_series)
            span_hours = (
                (x_dt.max() - x_dt.min()).total_seconds() / 3600 if len(x_dt) > 1 else 0
            )
            if datetime_formats is not None:
                fmt_short, fmt_medium, fmt_long = datetime_formats
            else:
                fmt_short, fmt_medium, fmt_long = ("%H:%M", "%m-%d %H:%M", "%Y-%m-%d")
            if span_hours < datetime_threshold_short_hours:
                fmt = fmt_short
            elif span_hours < datetime_threshold_medium_hours:
                fmt = fmt_medium
            else:
                fmt = fmt_long
            tick_labels = x_dt.iloc[tick_indices].dt.strftime(fmt)
        else:
            tick_labels = [str(x_series.iloc[idx]) for idx in tick_indices]

        tick_positions = x_numeric[tick_indices]
        ax.set_xticks(tick_positions)
        ax.set_xticklabels(tick_labels, rotation=x_tick_rotation, ha="right")

    default_legend_kwargs = {
        "loc": "upper center",
        "bbox_to_anchor": (0.5, legend_offset),
        "ncol": legend_ncol if legend_ncol is not None else max(1, min(4, len(labels))),
    }
    legend_params = {**default_legend_kwargs, **(legend_kwargs or {})}

    # Apply final styling and push legend configuration through style helper.
    style_axes(
        ax,
        title=title,
        xlabel=xlabel,
        ylabel=ylabel,
        grid=True,
        legend=legend,
        legend_kwargs=legend_params,
    )

    legend_obj = ax.get_legend()
    if legend and legend_obj is not None and ax.figure is not None:
        bbox = legend_params.get("bbox_to_anchor", (None, None))
        offset_y = legend_offset
        if isinstance(bbox, tuple) and len(bbox) >= 2 and bbox[1] is not None:
            offset_y = bbox[1]
        if offset_y is not None and offset_y < 0:
            extra_margin = max(min_extra_margin, -(offset_y + legend_baseline_offset))
            ax.figure.subplots_adjust(bottom=subplot_bottom_base + extra_margin)

    return ax


def plot_gpu_utilization_area(
    df,
    time_col: str = "Datetime",
    gpu_columns: Optional[list] = None,
    ax: Optional[plt.Axes] = None,
    colors: Optional[list] = None,
    title: str = "GPU Availability Over Time",
    xlabel: str = "Time",
    ylabel: str = "Number of Available GPUs",
    format_type: str = "double_column_wide",
    legend_offset: float = -0.25,
    legend: bool = True,
    legend_kwargs: Optional[Dict[str, Any]] = None,
    exclude_cols: Optional[List[str]] = None,
    label_formatter: Optional[Callable[[str], str]] = None,
    sort_x: bool = True,
    palette_name: str = "colorblind",
    **kwargs,
) -> plt.Axes:
    """
    Create a stacked area plot for GPU utilization over time.

    Parameters
    ----------
    df : pandas.DataFrame
        DataFrame containing GPU utilization data
    time_col : str, default 'Datetime'
        Column name for timestamps
    gpu_columns : list, optional
        List of GPU type columns to plot. If None, auto-detects GPU columns
    ax : matplotlib.axes.Axes, optional
        Axes object to plot on
    colors : list, optional
        Colors for each GPU type
    title : str, default 'GPU Availability Over Time'
        Plot title
    xlabel : str, default 'Time'
        X-axis label
    ylabel : str, default 'Number of Available GPUs'
        Y-axis label
    format_type : str, default 'double_column_wide'
        Figure format type
    legend_offset : float, default -0.25
        Vertical offset for the legend (negative values place it below the plot).
    **kwargs
        Additional arguments passed to fill_between()

    Returns
    -------
    ax : matplotlib.axes.Axes
        Axes object with the GPU utilization plot.
    """
    import pandas as pd

    # Auto-detect GPU columns if not provided
    if gpu_columns is None:
        # Common GPU column patterns - exclude non-GPU columns
        exclude_cols = exclude_cols or [
            "Datetime",
            "gpu",
            "Grand Total",
            "Total",
            "total",
        ]
        gpu_columns = [
            col
            for col in df.columns
            if col not in exclude_cols and "gpu" not in col.lower()
        ]

    if not gpu_columns:
        raise ValueError("No GPU columns found. Specify gpu_columns parameter.")

    # Ensure the timestamp column exists before delegating to the generic helper
    if time_col not in df.columns:
        raise ValueError(f"Time column '{time_col}' not found in DataFrame")

    if label_formatter is None:
        label_formatter = lambda name: " ".join(
            name.replace("_", " ").replace("nvidia", "").replace("geforce", "").split()
        ).title()

    df_processed = df.copy()
    df_processed[time_col] = pd.to_datetime(df_processed[time_col], errors="coerce")

    return plot_stacked_area_plot(
        df=df_processed,
        x_col=time_col,
        y_columns=gpu_columns,
        ax=ax,
        colors=colors,
        label_formatter=label_formatter,
        title=title,
        xlabel=xlabel,
        ylabel=ylabel,
        format_type=format_type,
        legend=legend,
        legend_kwargs=legend_kwargs,
        legend_offset=legend_offset,
        sort_x=sort_x,
        palette_name=palette_name,
        **kwargs,
    )


def human_readable_format(x, pos, precision: int = 0):
    """Format large/small values using SI-friendly suffixes (e.g., 20.0M)."""
    if x == 0:
        return "0"

    magnitude = abs(x)
    thresholds = (
        (1e12, "T"),
        (1e9, "B"),
        (1e6, "M"),
        (1e3, "K"),
    )

    for threshold, suffix in thresholds:
        if magnitude >= threshold:
            value = x / threshold
            return f"{value:.{precision}f}{suffix}"

    if magnitude >= 1:
        return f"{x:.{precision}f}"
    if magnitude >= 1e-3:
        return f"{x * 1e3:.{precision}f}m"

    return f"{x:.{precision}e}"

def ensure_min_xticks(ax, formatter, min_ticks=2):
    # 1. Get current limits
    lo, hi = ax.get_xlim()

    # 2. Expand range if necessary
    if hi <= lo:
        hi = lo * 10

    # If the range is too tight, expand it multiplicatively
    if (np.log10(hi) - np.log10(lo)) < (min_ticks - 1):
        # Expand so we can place at least `min_ticks` log ticks
        factor = 10 ** (min_ticks - 1)
        ax.set_xlim(lo, lo * factor)
        lo, hi = ax.get_xlim()

    # 3. Create ticks explicitly
    ticks = 10 ** np.linspace(np.log10(lo), np.log10(hi), min_ticks)
    ax.set_xticks(ticks)

    # 4. Apply your formatter
    ax.xaxis.set_major_formatter(FuncFormatter(formatter))

    # 5. Remove scientific offset
    ax.xaxis.get_offset_text().set_visible(False)

set_publication_style()
