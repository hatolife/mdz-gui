package main

const developmentVersion = "v0.0.1"

// version は通常のWailsビルド前フックで生成された値を使用します。
// go testなどWailsを介さない場合だけ未ビルド表記になります。
var version = developmentVersion + "-unbuilt"
