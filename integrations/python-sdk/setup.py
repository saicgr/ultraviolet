from setuptools import setup, find_packages

setup(
    name="ultraviolet",
    version="0.1.0",
    description="Python SDK for Ultraviolet — multi-warehouse query proxy + BI platform.",
    long_description=open("README.md").read() if __import__("os").path.exists("README.md") else "",
    long_description_content_type="text/markdown",
    packages=find_packages(),
    python_requires=">=3.9",
    extras_require={"pg": ["psycopg[binary]>=3.1"]},
    classifiers=[
        "Development Status :: 3 - Alpha",
        "License :: Other/Proprietary License",
        "Programming Language :: Python :: 3",
    ],
)
