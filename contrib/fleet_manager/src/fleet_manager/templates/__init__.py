from pathlib import Path
from jinja2 import Environment, FileSystemLoader


_TEMPLATE_DIR = Path(__file__).parent


def render_template(name: str, variables: dict) -> str:
    """Render a Jinja2 template with the given variables."""
    env = Environment(
        loader=FileSystemLoader(str(_TEMPLATE_DIR)),
        keep_trailing_newline=True,
        trim_blocks=True,
        lstrip_blocks=True,
    )
    template = env.get_template(name)
    return template.render(**variables)
