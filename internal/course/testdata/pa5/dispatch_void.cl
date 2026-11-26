class Main inherits IO {
  next : Main;
  main() : Object { { out_string("before\n"); next.main(); } };
};
