-- 预售 seed 商品(与前端 mock 数据一致;ID 为预生成的 UUIDv7)
INSERT INTO products (id, name, description, price, currency, stock, image_url) VALUES
('01a181d1-d53a-7428-811c-e53c0548c643', 'LicheeRV Nano',
 'SG2002 RISC-V SoC 开发板,板载 0.5TOPS NPU 与 1GB 内存,适合边缘 AI 视觉应用。',
 21900, 'CNY', 100, NULL),
('01a181d1-d541-7878-b7c5-9e91dd9a6c50', 'Tang Nano 9K',
 'GW1NR-9 FPGA 板卡,9k LUT,板载 HDMI 与 BL616 无线芯片,FPGA 入门首选。',
 10900, 'CNY', 50, NULL),
('01a181d1-d541-76f3-939f-5b3eac92c10f', 'MaixCube',
 'K210 AI 开发套件,集成摄像头、屏幕与电池,开箱即跑人脸识别 Demo。',
 19800, 'CNY', 0, NULL);
