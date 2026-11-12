import asyncio


def loop_factory() -> asyncio.AbstractEventLoop:
    """Psycopg async needs a selector loop, including when running on Windows."""
    return asyncio.SelectorEventLoop()
