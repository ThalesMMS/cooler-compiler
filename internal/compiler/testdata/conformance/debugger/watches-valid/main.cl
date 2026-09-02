class Main {
  value : Int <- 2;
  down(n : Int) : Object {
    if n = 0 then value else down(n - 1) fi
  };
  main() : Object { down(3) };
};
